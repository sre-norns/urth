import {expect, test} from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import {spawn, spawnSync} from 'node:child_process'
import {generateKeyPairSync} from 'node:crypto'
import {mkdtempSync, readFileSync, rmSync} from 'node:fs'
import {tmpdir} from 'node:os'
import {join} from 'node:path'

// Issuance responses contain one-time secrets. Do not retain browser traces.
test.use({trace: 'off'})
test.skip(!process.env.URTH_ENROLLMENT_LIVE_E2E, 'Requires an isolated mounted API, database, NATS, website and CLI')
test('CLI and UI issue/revoke the same Runner tokens without ordinary secret disclosure', async ({page, browser}, info) => {
  test.skip(info.project.name !== 'desktop', 'One client lifecycle per isolated stack')
  test.setTimeout(120_000)
  const cli = process.env.URTH_E2E_CLI
  const endpoint = process.env.URTH_E2E_API_URL
  const apiLog = process.env.URTH_E2E_API_LOG
  if (!cli || !endpoint || !apiLog) throw new Error('Set URTH_E2E_CLI, URTH_E2E_API_URL and URTH_E2E_API_LOG')
  const email = process.env.URTH_E2E_EMAIL ?? 'admin@urth.example'
  const password = process.env.URTH_E2E_PASSWORD ?? 'urth-dev-password'
  const suffix = Date.now().toString(36)
  const runnerName = `clients-${suffix}`
  const dir = mkdtempSync(join(tmpdir(), 'm9-enrollment-clients-'))
  const env = {...process.env, URTH_PROFILES: join(dir, 'profiles.json')}
  const issued: string[] = []
  const ordinaryOutput: string[] = []
  const runtimeErrors: string[] = []
  page.on('pageerror', (error) => runtimeErrors.push(error.message))
  function excludesSecrets(value: string) {
    expect(issued.some((secret) => value.includes(secret)), 'Ordinary output excludes issued secrets').toBe(false)
  }
  function runCLI(args: string[], issuance = false, anonymous = false) {
    const result = spawnSync(cli!, args, {env: anonymous ? {...env, URTH_PROFILES: join(dir, 'anonymous.json')} : env, encoding: 'utf8'})
    if (result.error) throw new Error('CLI process did not start')
    if (!issuance) ordinaryOutput.push(result.stdout)
    ordinaryOutput.push(result.stderr)
    return result
  }
  const login = spawn(cli, ['auth', 'login', `--api-server-address=${endpoint}`], {env, stdio: ['ignore', 'pipe', 'pipe']})
  let loginOutput = ''
  login.stdout.on('data', (chunk: Buffer) => {loginOutput += chunk.toString()})
  login.stderr.on('data', (chunk: Buffer) => {loginOutput += chunk.toString()})
  const device = await browser.newPage()
  try {
    await expect.poll(() => loginOutput.match(/https?:\/\/[^\s]+\/oauth\/device[^\s]*/)?.[0], {timeout: 10_000}).toBeTruthy()
    await device.goto(loginOutput.match(/https?:\/\/[^\s]+\/oauth\/device[^\s]*/)![0])
    await device.getByLabel('Email', {exact: true}).fill(email)
    await device.getByLabel('Password', {exact: true}).fill(password)
    await device.getByRole('button', {name: 'Approve access'}).click()
    if (await device.locator('select[name="authority_context"]').isVisible()) {
      const authority = await device.locator('select[name="authority_context"] option').evaluateAll((options) => options.map((option) => (option as HTMLOptionElement).value).find(Boolean))
      await device.locator('select[name="authority_context"]').selectOption(authority!)
      await device.getByRole('button', {name: 'Approve access'}).click()
    }
    await expect.poll(() => login.exitCode, {timeout: 15_000}).toBe(0)
    await device.close()
    await page.goto('/sign-in')
    await page.getByLabel('Email', {exact: true}).fill(email)
    await page.getByLabel('Password', {exact: true}).fill(password)
    await page.getByRole('button', {name: 'Log in', exact: true}).click()
    await page.waitForURL(/\/a\//)
    const accountBase = new URL(page.url()).pathname.match(/^\/a\/[^/]+/)![0]
    const accountID = accountBase.split('/').at(-1)!
    await page.goto(`${accountBase}/runners`)
    await page.getByRole('button', {name: 'Register runner', exact: true}).click()
    await page.getByLabel('Runner name').fill(runnerName)
    await page.getByRole('dialog').getByRole('button', {name: 'Save', exact: true}).click()
    await page.waitForURL(/\/runners\//)
    const runnerURL = new URL(page.url()).pathname
    const key = `client-issue-${suffix}`
    const expiresAt = new Date(Date.now() + 86_400_000).toISOString().replace(/\.\d{3}Z$/, 'Z')
    const created = runCLI(['runners', 'tokens', 'issue', runnerName, 'cli-installation', `--idempotency-key=${key}`, `--expires-at=${expiresAt}`, '-o', 'json'], true)
    expect(created.status).toBe(0)
    const creation = JSON.parse(created.stdout) as {token?: string; resource: {metadata: {uid: string}}}
    if (!creation.token) throw new Error('Authenticated CLI issuance did not return a one-time secret')
    issued.push(creation.token)
    const cliID = creation.resource.metadata.uid
    const expiryRead = runCLI(['runners', 'tokens', 'get', cliID, '-o', 'json'])
    expect(expiryRead.status).toBe(0)
    expect(Date.parse(JSON.parse(expiryRead.stdout).spec.expiresAt)).toBe(Date.parse(expiresAt))
    const replay = runCLI(['runners', 'tokens', 'issue', runnerName, 'cli-installation', `--idempotency-key=${key}`, `--expires-at=${expiresAt}`, '-o', 'json'])
    expect(replay.status).toBe(0)
    expect(Boolean(JSON.parse(replay.stdout).token), 'Issuance replay does not return the secret').toBe(false)
    const {publicKey} = generateKeyPairSync('ed25519')
    const publicKeyText = publicKey.export({format: 'der', type: 'spki'}).subarray(-32).toString('base64url')
    async function challenge(secret: string, expected: number) {
      const response = await page.request.post(`${endpoint}/v1/auth/workers/challenge`, {
        headers: {Authorization: `Bearer ${secret}`},
        data: {apiVersion: 'urth.sre-norns.com/v1', kind: 'workerInstances', metadata: {name: `proof-${suffix}`}, spec: {proof: {publicKey: publicKeyText}}},
      })
      expect(response.status(), 'Mounted Worker challenge checks token authority').toBe(expected)
    }
    await challenge(creation.token, 200)
    await page.getByRole('button', {name: 'Manage tokens', exact: true}).click()
    const cliRow = page.getByRole('row').filter({hasText: 'cli-installation'})
    await expect(cliRow).toBeVisible()
    await cliRow.getByRole('button', {name: 'Revoke', exact: true}).click()
    await page.getByRole('dialog', {name: 'Revoke identity token', exact: true}).getByRole('button', {name: 'Save', exact: true}).click()
    await expect(cliRow.getByText('revoked', {exact: true})).toBeVisible()
    const revokedRead = runCLI(['runners', 'tokens', 'get', cliID, '-o', 'json'])
    expect(revokedRead.status).toBe(0)
    expect(JSON.parse(revokedRead.stdout).status.phase).toBe('revoked')
    await challenge(creation.token, 401)
    await page.getByRole('button', {name: 'Create token', exact: true}).click()
    await page.getByLabel('Token name').fill('ui-installation')
    await page.getByRole('dialog', {name: `Identity tokens · ${runnerName}`, exact: true}).getByRole('button', {name: 'Save', exact: true}).click()
    const secretField = page.getByLabel('New identity token', {exact: true})
    await expect(secretField).toBeVisible()
    const uiSecret = await secretField.inputValue()
    if (!uiSecret) throw new Error('Authenticated UI issuance did not show its one-time secret')
    issued.push(uiSecret)
    await challenge(uiSecret, 200)
    await page.getByRole('button', {name: 'Close window', exact: true}).click()
    await expect(secretField).toHaveCount(0)
    excludesSecrets(await page.content())
    const listed = runCLI(['runners', 'tokens', 'list', runnerName, '-o', 'json'])
    expect(listed.status).toBe(0)
    const tokenList = JSON.parse(listed.stdout) as {metadata: {uid: string; name: string}}[]
    const uiID = tokenList.find((item) => item.metadata.name === 'ui-installation')?.metadata.uid
    if (!uiID) throw new Error('CLI cannot read UI-issued token metadata')
    expect(runCLI(['runners', 'tokens', 'revoke', uiID, '-o', 'json']).status).toBe(0)
    await challenge(uiSecret, 401)
    await page.reload()
    await page.getByRole('button', {name: 'Manage tokens', exact: true}).click()
    await expect(page.getByRole('row').filter({hasText: 'ui-installation'}).getByText('revoked', {exact: true})).toBeVisible()
    excludesSecrets(await page.content())
    excludesSecrets(await page.evaluate(() => JSON.stringify({local: {...localStorage}, session: {...sessionStorage}})))
    for (const format of ['table', 'wide', 'json', 'yaml']) {
      expect(runCLI(['runners', 'tokens', 'list', runnerName, '-o', format]).status).toBe(0)
      expect(runCLI(['runners', 'tokens', 'get', uiID, '-o', format]).status).toBe(0)
      expect(runCLI(['get', 'runner', runnerName, '-o', format]).status).toBe(0)
    }
    const missing = runCLI(['runners', 'tokens', 'get', '00000000-0000-4000-8000-000000000000'])
    expect(missing.status).not.toBe(0)
    const denied = runCLI(['runners', 'tokens', 'issue', runnerName, 'anonymous', `--api-server-address=${endpoint}`, `--account=${accountID}`], false, true)
    expect(denied.status).not.toBe(0)
    const legacy = runCLI(['runners', 'token', runnerName], true)
    expect(legacy.status).toBe(0)
    const legacySecret = legacy.stdout.trim()
    if (!legacySecret) throw new Error('Existing runners token command lost its one-time output')
    issued.push(legacySecret)
    await challenge(legacySecret, 200)
    const legacyList = runCLI(['runners', 'tokens', 'list', runnerName, '-o', 'json'])
    const legacyID = JSON.parse(legacyList.stdout).find((item: {metadata: {uid: string}}) => ![cliID, uiID].includes(item.metadata.uid))?.metadata.uid
    if (!legacyID) throw new Error('Legacy-issued token has no shared metadata record')
    expect(runCLI(['runners', 'tokens', 'revoke', legacyID]).status).toBe(0)
    await challenge(legacySecret, 401)
    for (const output of [...ordinaryOutput, loginOutput, ...runtimeErrors, readFileSync(apiLog, 'utf8'), readFileSync(env.URTH_PROFILES, 'utf8')]) excludesSecrets(output)
    expect(runtimeErrors.length).toBe(0)
    await page.goto(runnerURL)
    await expect(page.getByRole('heading', {name: runnerName, exact: true})).toBeVisible()
    expect((await new AxeBuilder({page}).analyze()).violations.map(({id, nodes}) => ({id, targets: nodes.map(({target}) => target)}))).toEqual([])
    await page.setViewportSize({width: 420, height: 844})
    await page.getByRole('button', {name: 'Manage tokens', exact: true}).click()
    expect((await new AxeBuilder({page}).analyze()).violations.map(({id, nodes}) => ({id, targets: nodes.map(({target}) => target)}))).toEqual([])
    await page.screenshot({path: process.env.URTH_E2E_CLIENT_SCREENSHOT ?? info.outputPath('tokens-revoked-narrow.png')})
  } finally {
    if (login.exitCode === null && login.signalCode === null) {
      login.kill('SIGTERM')
      await new Promise<void>((resolve) => login.once('exit', () => resolve()))
    }
    await device.close()
    rmSync(dir, {recursive: true, force: true})
  }
})
