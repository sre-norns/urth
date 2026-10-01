import {expect, test} from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import {spawn} from 'node:child_process'
import {mkdtempSync, writeFileSync, rmSync} from 'node:fs'
import {tmpdir} from 'node:os'
import {join} from 'node:path'

test.skip(!process.env.URTH_LIVE_E2E, 'Requires an isolated API, database, NATS and built worker; see README')
test('login → create project → grant runner → execute scenario → logs and artifacts', async ({page}, info) => {
  test.skip(info.project.name !== 'desktop', 'One worker execution per live stack')
  test.setTimeout(150_000)
  const workerBinary = process.env.URTH_E2E_WORKER
  if (!workerBinary) throw new Error('Set URTH_E2E_WORKER to a built nats-worker binary')
  const suffix = Date.now().toString(36)
  const runnerName = `m7-${suffix}`
  const scenarioName = `check-${suffix}`
  const runtimeErrors: string[] = []
  page.on('pageerror', (error) => runtimeErrors.push(error.message))
  await page.goto('/')
  await page.getByLabel('Email', {exact: true}).fill(process.env.URTH_E2E_EMAIL ?? 'admin@urth.example')
  await page.getByLabel('Password', {exact: true}).fill(process.env.URTH_E2E_PASSWORD ?? 'urth-dev-password')
  await page.getByRole('button', {name: 'Log in', exact: true}).click()
  await page.waitForURL(/\/a\//)
  await page.getByRole('link', {name: 'Create project', exact: true}).click()
  await page.getByLabel('Project name').fill(`M7 ${suffix}`)
  await page.getByRole('button', {name: 'Create project', exact: true}).click()
  await page.waitForURL(/\/p\//)
  const base = new URL(page.url()).pathname.replace(/\/members$/, '')
  const accountBase = base.split('/p/')[0]!
  const accountID = accountBase.split('/')[2]!
  const projectID = base.split('/p/')[1]!
  await page.goto(`${accountBase}/runners`)
  await page.getByRole('button', {name: 'Register runner', exact: true}).click()
  await page.getByLabel('Runner name').fill(runnerName)
  await page.getByRole('dialog').getByRole('button', {name: 'Save', exact: true}).click()
  await page.waitForURL(/\/runners\//)
  const runnerURL = new URL(page.url()).pathname
  const runnerID = runnerURL.split('/').at(-1)!
  await page.goto(`${base}/runners`)
  await page.getByRole('button', {name: 'Authorize runner', exact: true}).click()
  await page.getByRole('dialog').getByLabel('Search account runners').fill(runnerName)
  await page.getByRole('button', {name: new RegExp(`${runnerName}[\\s\\S]*Select runner`)}).click()
  await page.getByRole('button', {name: 'Add Runner', exact: true}).click()
  await page.getByRole('dialog').getByRole('button', {name: 'Authorize runner', exact: true}).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  // Bootstrap a real process using the product's one-time token endpoint.
  // Credentials remain in a private temporary file and never enter argv.
  const token = await page.evaluate(async ({accountID, runnerName}) => {
    const session = JSON.parse(sessionStorage.getItem('urth.session')!) as {access_token: string}
    const response = await fetch(`/v1/accounts/${accountID}/runners/${runnerName}/tokens`, {method: 'POST', headers: {Authorization: `Bearer ${session.access_token}`}})
    if (!response.ok) throw new Error(`Worker token: ${response.status}`)
    return response.text()
  }, {accountID, runnerName})
  const dir = mkdtempSync(join(tmpdir(), 'm7-e2e-'))
  const tokenFile = join(dir, 'token')
  writeFileSync(tokenFile, token, {mode: 0o600})
  const workerName = `${runnerName}-worker`
  const worker = spawn(workerBinary, [
    `--token-file=${tokenFile}`, `--name=${workerName}`, `--working-directory=${dir}`,
    `--client.api-server-address=${process.env.URTH_E2E_API_URL ?? 'http://127.0.0.1:18087'}`,
    `--nats.url=${process.env.URTH_E2E_NATS_URL ?? 'nats://127.0.0.1:14227'}`,
  ], {stdio: 'ignore'})
  try {
    await page.goto(`${base}/scenarios/new`)
    await page.getByLabel('Name', {exact: true}).fill(scenarioName)
    const target = process.env.URTH_E2E_PROBE_URL ?? 'http://127.0.0.1:18087/v1/version'
    await page.getByLabel(/Probe specification/).fill(`target: ${target}\nhttp:\n  method: GET\n  IPProtocolFallback: true\n`)
    await page.getByRole('button', {name: 'Save', exact: true}).click()
    await page.waitForURL(new RegExp(`/scenarios/${scenarioName}$`))
    await page.getByRole('button', {name: 'Run now', exact: true}).click()
    await page.waitForURL(/\/runs\//)
    await expect(page.getByLabel('Run log', {exact: true})).toContainText('Probe succeeded', {timeout: 60_000})
    await expect(page.getByText('success', {exact: true})).toBeVisible({timeout: 30_000})
    const runURL = new URL(page.url()).pathname
    await page.screenshot({path: info.outputPath('live-run.png'), fullPage: true})
    await page.getByRole('link', {name: 'Run artifacts', exact: true}).click()
    await expect(page.getByRole('table')).toBeVisible()
    const artifactURL = await page.locator('tbody a').first().getAttribute('href')
    const routes = [
      `${accountBase}/projects`, `${accountBase}/projects/new`, `${accountBase}/runners`, runnerURL,
      `${accountBase}/workers`, `${accountBase}/workers/${workerName}`, `${accountBase}/dead-letters`,
      `${accountBase}/members`, `${accountBase}/invitations`, `${accountBase}/settings`, '/profile', '/account/sessions',
      `${base}/members`, `${base}/runners`, `${base}/runners/${runnerID}`, `${base}/scenarios`, `${base}/scenarios/new`,
      `${base}/scenarios/${scenarioName}`, `${base}/scenarios/${scenarioName}/edit`, `${base}/scenarios/${scenarioName}/runs`,
      `${base}/runs`, runURL, `${base}/artifacts`, artifactURL!, `${base}/dead-letters`,
    ]
    for (const route of routes) {
      await page.goto(route)
      await expect(page.locator('h1').first(), route).toBeVisible()
      await expect(page.getByRole('status', {name: 'Loading', exact: true})).toHaveCount(0)
      expect((await new AxeBuilder({page}).analyze()).violations, route).toEqual([])
    }
    expect(runtimeErrors).toEqual([])
    // The stream must remain inaccessible without the user's bearer token.
    const runName = runURL.split('/').at(-1)!
    const anonymous = await page.request.get(`/v1/projects/${projectID}/scenarios/${scenarioName}/results/${runName}/logs`, {headers: {Accept: 'text/event-stream'}})
    expect(anonymous.status()).toBe(401)
  } finally {
    worker.kill('SIGTERM')
    await new Promise<void>((resolve) => {if (worker.exitCode !== null) resolve(); else worker.once('exit', () => resolve())})
    rmSync(dir, {recursive: true, force: true})
  }
})
