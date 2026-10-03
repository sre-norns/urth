import {expect, test} from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import {spawn} from 'node:child_process'
import {mkdtempSync, writeFileSync, rmSync} from 'node:fs'
import {tmpdir} from 'node:os'
import {join} from 'node:path'
import {createServer, type ServerResponse} from 'node:http'

test.skip(!process.env.URTH_LIVE_E2E, 'Requires an isolated API, database, NATS and built worker; see README')
test('login → create project → grant runner → execute scenario → logs and artifacts', async ({page}, info) => {
  test.skip(info.project.name !== 'desktop', 'One worker execution per live stack')
  test.setTimeout(150_000)
  const workerBinary = process.env.URTH_E2E_WORKER
  if (!workerBinary) throw new Error('Set URTH_E2E_WORKER to a built nats-worker binary')
  const suffix = Date.now().toString(36)
  const runnerName = `live-${suffix}`
  const scenarioName = `check-${suffix}`
  const runtimeErrors: string[] = []
  page.on('pageerror', (error) => runtimeErrors.push(error.message))
  await page.goto('/')
  await page.getByLabel('Email', {exact: true}).fill(process.env.URTH_E2E_EMAIL ?? 'admin@urth.example')
  await page.getByLabel('Password', {exact: true}).fill(process.env.URTH_E2E_PASSWORD ?? 'urth-dev-password')
  await page.getByRole('button', {name: 'Log in', exact: true}).click()
  await page.waitForURL(/\/a\//)
  await page.getByRole('link', {name: 'Create project', exact: true}).click()
  await page.getByLabel('Project name').fill(`Live ${suffix}`)
  await page.getByRole('button', {name: 'Create project', exact: true}).click()
  await page.waitForURL(/\/p\//)
  const base = new URL(page.url()).pathname.replace(/\/members$/, '')
  const accountBase = base.split('/p/')[0]!
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
  const credential = await page.evaluate(async ({runnerID, runnerName}) => {
    const session = JSON.parse(sessionStorage.getItem('urth.session')!) as {access_token: string}
    const path = `/v1/agent-identities/${runnerID}/tokens`
    const headers = {Authorization: `Bearer ${session.access_token}`, 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID()}
    const body = JSON.stringify({apiVersion: 'identity.sre-norns.com/v1', kind: 'agent-identity-tokens', metadata: {name: runnerName}, spec: {}})
    const response = await fetch(path, {method: 'POST', headers, body})
    if (!response.ok) throw new Error(`Worker token: ${response.status}`)
    const result = await response.json() as {token?: string; resource: {metadata: {uid: string}}}
    if (!result.token) throw new Error('Token secret unavailable')
    for (const read of [await fetch(`/v1/agent-identity-tokens/${result.resource.metadata.uid}`, {headers}), await fetch(path, {method: 'POST', headers, body})]) {
      if (!read.ok || (await read.text()).includes(result.token)) throw new Error('Token read/replay exposes the issued secret')
    }
    return {token: result.token, id: result.resource.metadata.uid}
  }, {runnerID, runnerName})
  const token = credential.token
  const dir = mkdtempSync(join(tmpdir(), 'urth-live-e2e-'))
  const tokenFile = join(dir, 'token')
  writeFileSync(tokenFile, token, {mode: 0o600})
  const workerName = `${runnerName}-worker`
  const probePort = process.env.URTH_E2E_SLOW_PROBE_PORT
  let heldProbe: ServerResponse | undefined
  let firstProbe = true
  const probeServer = probePort ? createServer((_request, response) => {
    if (firstProbe) {firstProbe = false; heldProbe = response}
    else response.end('ok')
  }) : undefined
  if (probeServer) await new Promise<void>((resolve, reject) => {
    probeServer.once('error', reject)
    probeServer.listen(Number(probePort), '127.0.0.1', resolve)
  })
  const worker = spawn(workerBinary, [
    `--token-file=${tokenFile}`, `--name=${workerName}`, `--working-directory=${dir}`,
    `--identity-key-file=${join(dir, 'worker.key')}`,
    '--allow-insecure-api', '--nats.allow-insecure',
    `--client.api-server-address=${process.env.URTH_E2E_API_URL ?? 'http://127.0.0.1:18087'}`,
    `--nats.url=${process.env.URTH_E2E_NATS_URL ?? 'nats://127.0.0.1:14227'}`,
  ], {stdio: ['ignore', 'pipe', 'pipe']})
  let workerOutput = ''
  worker.stdout.on('data', (chunk: Buffer) => {workerOutput = (workerOutput + chunk.toString()).slice(-16_000)})
  worker.stderr.on('data', (chunk: Buffer) => {workerOutput = (workerOutput + chunk.toString()).slice(-16_000)})
  try {
    await page.goto(`${base}/scenarios/new`)
    await page.getByLabel('Name', {exact: true}).fill(scenarioName)
    const target = probePort ? `http://127.0.0.1:${probePort}/probe` : process.env.URTH_E2E_PROBE_URL ?? 'http://127.0.0.1:18087/v1/version'
    await page.getByLabel(/Probe specification/).fill(`target: ${target}\nhttp:\n  method: GET\n  IPProtocolFallback: true\n`)
    await page.getByRole('button', {name: 'Save', exact: true}).click()
    await page.waitForURL(new RegExp(`/scenarios/${scenarioName}$`))
    await page.getByRole('button', {name: 'Run now', exact: true}).click()
    await page.waitForURL(/\/runs\//)
    if (probeServer) {
      await expect.poll(() => Boolean(heldProbe), {timeout: 15_000}).toBe(true)
      await expect(page.getByText('running', {exact: true})).toBeVisible({timeout: 20_000})
      // The development proxy can deliver headers with the first keepalive.
      await expect(page.getByText('live', {exact: true})).toBeVisible({timeout: 20_000})
      await page.screenshot({path: info.outputPath('live-running.png'), fullPage: true})
      heldProbe!.end('ok')
    }
    await expect(page.getByLabel('Run log', {exact: true})).toContainText('Probe succeeded', {timeout: 60_000})
    await expect(page.getByText('success', {exact: true})).toBeVisible({timeout: 30_000})
    const runURL = new URL(page.url()).pathname
    await page.screenshot({path: info.outputPath('live-run.png'), fullPage: true})
    await page.setViewportSize({width: 390, height: 844})
    await page.screenshot({path: info.outputPath('live-run-narrow.png'), fullPage: true})
    await page.setViewportSize({width: 1280, height: 720})
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
    expect((await page.request.post(`/v1/agent-identities/${runnerID}/tokens`, {data: {apiVersion: 'identity.sre-norns.com/v1', kind: 'agent-identity-tokens', metadata: {name: 'unauthorized'}, spec: {}}})).status()).toBe(401)
    // The real Worker receives a dispatch but cannot claim while its key is blocked.
    const block = await page.evaluate(async ({accountBase, projectID, runnerName, workerName, scenarioName}) => {
      const session = JSON.parse(sessionStorage.getItem('urth.session')!) as {access_token: string}
      const headers = {Authorization: `Bearer ${session.access_token}`, 'Content-Type': 'application/json'}
      const accountID = accountBase.split('/').at(-1)!
      const path = `/v1/accounts/${accountID}/runners/${runnerName}`
      const read = await fetch(path, {headers})
      const source = await read.json()
      const worker = await (await fetch(`/v1/accounts/${accountID}/workers/${workerName}`, {headers})).json()
      if (!worker.status?.fingerprint) throw new Error('Worker has no verified fingerprint')
      source.spec.blockedWorkers = [{identity: worker.status.fingerprint, reason: 'Live validation'}]
      const options = {method: 'PUT', headers: {...headers, 'If-Match': read.headers.get('ETag')!}, body: JSON.stringify(source)}
      const blocked = await fetch(path, options)
      if (!blocked.ok) throw new Error(`Block: ${blocked.status}`)
      const stale = await fetch(path, options)
      if (![409, 412].includes(stale.status)) throw new Error(`Stale block write: ${stale.status}`)
      const trigger = await fetch(`/v1/projects/${projectID}/scenarios/${scenarioName}/results`, {method: 'POST', headers, body: JSON.stringify({apiVersion: 'urth.sre-norns.com/v1', kind: 'results', metadata: {}, spec: {}})})
      if (!trigger.ok) throw new Error(`Blocked trigger: ${trigger.status}`)
      return {uid: (await trigger.json()).metadata.uid, path}
    }, {accountBase, projectID, runnerName, workerName, scenarioName})
    await expect.poll(() => workerOutput, {timeout: 15_000, message: 'Blocked Worker receives an explicit claim refusal'}).toMatch(new RegExp(`claim for result ${block.uid}:.*(?:403|forbidden|blocked)`, 'i'))
    const resumed = await page.evaluate(async ({path, projectID, scenarioName}) => {
      const session = JSON.parse(sessionStorage.getItem('urth.session')!) as {access_token: string}
      const headers = {Authorization: `Bearer ${session.access_token}`, 'Content-Type': 'application/json'}
      const read = await fetch(path, {headers})
      const source = await read.json()
      source.spec.blockedWorkers = []
      const unblocked = await fetch(path, {method: 'PUT', headers: {...headers, 'If-Match': read.headers.get('ETag')!}, body: JSON.stringify(source)})
      if (!unblocked.ok) throw new Error(`Unblock: ${unblocked.status}`)
      const trigger = await fetch(`/v1/projects/${projectID}/scenarios/${scenarioName}/results`, {method: 'POST', headers, body: JSON.stringify({apiVersion: 'urth.sre-norns.com/v1', kind: 'results', metadata: {}, spec: {}})})
      if (!trigger.ok) throw new Error(`Unblocked trigger: ${trigger.status}`)
      return (await trigger.json()).metadata.name as string
    }, {path: block.path, projectID, scenarioName})
    await page.goto(`${base}/runs/${resumed}`)
    await expect(page.getByText('success', {exact: true})).toBeVisible({timeout: 30_000})
    // Removing the last project grant refuses placement while the Worker is alive.
    await page.evaluate(async ({projectID, scenarioName}) => {
      const session = JSON.parse(sessionStorage.getItem('urth.session')!) as {access_token: string}
      const headers = {Authorization: `Bearer ${session.access_token}`, 'Content-Type': 'application/json'}
      const root = `/v1/projects/${projectID}`
      const grants = await (await fetch(`${root}/runner-authorizations`, {headers})).json()
      if (grants.items.length !== 1) throw new Error('Expected one project grant')
      const path = `${root}/runner-authorizations/${grants.items[0].metadata.name}`
      const read = await fetch(path, {headers})
      const grant = await read.json()
      const deleted = await fetch(`${path}?version=${grant.metadata.version}`, {method: 'DELETE', headers: {...headers, 'If-Match': read.headers.get('ETag')!}})
      if (!deleted.ok) throw new Error(`Grant revoke: ${deleted.status}`)
      const trigger = await fetch(`${root}/scenarios/${scenarioName}/results`, {method: 'POST', headers, body: JSON.stringify({apiVersion: 'urth.sre-norns.com/v1', kind: 'results', metadata: {}, spec: {}})})
      if (!trigger.ok) throw new Error(`Revoked-grant trigger: ${trigger.status}`)
      const result = await trigger.json()
      if (result.status?.status !== 'errored' || result.metadata.labels?.['urth/result.unschedulable'] !== 'no-eligible-runner') throw new Error('Revoked grant still permits placement')
    }, {projectID, scenarioName})
    // Revocation uses the mounted shared operation and the read precondition.
    await page.evaluate(async (id) => {
      const session = JSON.parse(sessionStorage.getItem('urth.session')!) as {access_token: string}
      const headers = {Authorization: `Bearer ${session.access_token}`}
      const path = `/v1/agent-identity-tokens/${id}`
      const read = await fetch(path, {headers})
      if (!read.ok) throw new Error(`Token read: ${read.status}`)
      const revoked = await fetch(path, {method: 'PATCH', headers: {...headers, 'Content-Type': 'application/json', 'If-Match': read.headers.get('ETag')!}, body: JSON.stringify({operation: 'revoke'})})
      if (!revoked.ok) throw new Error(`Token revocation: ${revoked.status}`)
    }, credential.id)
    const refusedWorker = spawn(workerBinary, [
      `--token-file=${tokenFile}`, `--name=${workerName}-revoked`, `--working-directory=${dir}`,
      `--identity-key-file=${join(dir, 'refused.key')}`, '--allow-insecure-api', '--nats.allow-insecure',
      `--client.api-server-address=${process.env.URTH_E2E_API_URL ?? 'http://127.0.0.1:18087'}`,
      '--api-registration-timeout=5s',
    ], {stdio: ['ignore', 'pipe', 'pipe']})
    let refusedOutput = ''
    refusedWorker.stdout.on('data', (chunk: Buffer) => {refusedOutput += chunk.toString()})
    refusedWorker.stderr.on('data', (chunk: Buffer) => {refusedOutput += chunk.toString()})
    try {
      await expect.poll(() => refusedWorker.exitCode, {timeout: 10_000}).not.toBeNull()
      expect(refusedWorker.exitCode).not.toBe(0)
      expect(refusedOutput.replaceAll(token, '[redacted]')).toMatch(/401|unauthenticated|invalid|revoked|permission/i)
    } finally {
      if (refusedWorker.exitCode === null && refusedWorker.signalCode === null) {
        refusedWorker.kill('SIGTERM')
        await new Promise<void>((resolve) => refusedWorker.once('exit', () => resolve()))
      }
    }
  } catch (error) {
    // Keep secrets out of reports, including unexpected startup diagnostics.
    throw new Error(`${error instanceof Error ? error.message : String(error)}\nWorker diagnostics:\n${workerOutput.replaceAll(token, '[redacted]')}`)
  } finally {
    if (worker.exitCode === null && worker.signalCode === null) worker.kill('SIGTERM')
    await new Promise<void>((resolve) => {if (worker.exitCode !== null || worker.signalCode !== null) resolve(); else worker.once('exit', () => resolve())})
    rmSync(dir, {recursive: true, force: true})
    if (probeServer) {
      probeServer.closeAllConnections()
      await new Promise<void>((resolve) => probeServer.close(() => resolve()))
    }
  }
})
