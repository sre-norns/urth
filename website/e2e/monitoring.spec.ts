import {expect, test} from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import {identityResource} from '../src/test/identity-fixtures'

const stamp = '2026-10-01T00:00:00Z'
const account = 'acct-1', project = 'proj-1'
const base = `/a/${account}/p/${project}`

const metadata = {name: 'checkout-health', uid: 'scenario-1', version: 1, account, project}
const scenario = {apiVersion: 'urth.sre-norns.com/v1', kind: 'scenarios', metadata, spec: {active: true, description: 'Checkout service health', prob: {kind: 'http'}}, status: {}}
const run = {apiVersion: 'urth.sre-norns.com/v1', kind: 'results', metadata: {...metadata, name: 'run-1', uid: 'run-1', labels: {'urth/scenario.name': 'checkout-health'}}, spec: {probKind: 'http'}, status: {status: 'completed', result: 'success'}}
const pageOf = (items: unknown[]) => ({items, limit: 20, total: items.length})

test.skip(Boolean(process.env.URTH_LIVE_E2E), 'Mock route suite; live mode runs live.spec.ts')
test.beforeEach(async ({page}) => {
  await page.addInitScript(({account}) => sessionStorage.setItem('urth.session', JSON.stringify({access_token: 'access', refresh_token: 'refresh', token_type: 'Bearer', scope: 'account', account_id: account, expires_in: 900, expires_at: Date.now() + 900_000})), {account})
  await page.route('**/v1/**', async (route) => {
    expect(route.request().headers()['authorization']).toBe('Bearer access')
    const url = new URL(route.request().url())
    const path = url.pathname
    const json = (body: unknown) => route.fulfill({json: body})
    if (path === '/v1/principal') return json({type: 'user', user_id: 'user-1', account_id: account, credential_id: 'sess-1', account_role: 'owner', project_ids: [project], system_admin: false})
    if (path === '/v1/profile') return json(identityResource('personal-profiles', 'user-1', {displayName: 'Ada'}, {email: 'ada@example.test'}))
    if (path === '/v1/profile/accounts') return json(pageOf([{account_id: account, name: 'Edge monitoring', role: 'owner', status: 'active'}]))
    if (path === `/v1/accounts/${account}`) return json(identityResource('accounts', account, {description: ''}, {}, {name: 'Edge monitoring'}))
    if (path === `/v1/accounts/${account}/projects`) return json(pageOf([identityResource('projects', project, {description: ''}, {}, {name: 'Plant 2'})]))
    if (path === `/v1/projects/${project}`) return json(identityResource('projects', project, {description: ''}, {}, {name: 'Plant 2'}))
    if (path.endsWith('/scenarios')) return json(pageOf([scenario]))
    if (path.endsWith('/scenarios/checkout-health')) return route.fulfill({json: scenario, headers: {ETag: '"1"'}})
    if (path.endsWith('/placement')) return json({schedulable: true, eligibleRunners: 1, readyWorkers: 2})
    if (path.endsWith('/results') && route.request().method() === 'POST') return json(run)
    if (path.endsWith('/results/run-1')) return json(run)
    if (path.endsWith('/results')) return json(pageOf([run]))
    if (path.endsWith('/logs')) return route.fulfill({contentType: 'text/event-stream', body: 'data: authenticated browser log\n\nevent: end\ndata: completed\n\n'})
    if (path.endsWith('/artifacts')) return json(pageOf([{apiVersion: 'urth.sre-norns.com/v1', kind: 'artifacts', metadata: {...metadata, name: 'trace-1'}, spec: {dataClass: 'secret-bearing'}}]))
    if (path.endsWith('/artifacts/trace-1')) return json({apiVersion: 'urth.sre-norns.com/v1', kind: 'artifacts', metadata: {...metadata, name: 'trace-1'}, spec: {dataClass: 'secret-bearing'}})
    if (path.endsWith('/artifacts/trace-1/content')) return route.fulfill({contentType: 'text/plain', body: 'sensitive trace'})
    if (path.endsWith('/dispatch-failures/failure-1')) return json({apiVersion: 'urth.sre-norns.com/v1', kind: 'dispatch-failures', metadata: {...metadata, name: 'failure-1'}, spec: {reason: 'delivery-exhausted', occurredAt: stamp, resultUID: 'run-1'}, status: {resolved: false}})
    throw new Error(`Unhandled browser request: ${route.request().method()} ${path}`)
  })
})
test('scenario → placement → run → authenticated log, with accessible layouts', async ({page}) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto(`${base}/scenarios`)
  await page.getByRole('link', {name: 'checkout-health', exact: true}).click()
  await page.getByRole('button', {name: 'Run now', exact: true}).click()
  await expect(page.getByLabel('Run log', {exact: true})).toContainText('authenticated browser log')
  expect((await new AxeBuilder({page}).analyze()).violations).toEqual([])
  expect(errors).toEqual([])
  await page.screenshot({path: test.info().outputPath('run.png'), fullPage: true})
})
test('a project card counts its scenarios and opens on them', async ({page}) => {
  await page.goto(`/a/${account}/projects`)
  await expect(page.getByText('1 scenario', {exact: true})).toBeVisible()
  expect((await new AxeBuilder({page}).analyze()).violations).toEqual([])
  await page.screenshot({path: test.info().outputPath('projects.png'), fullPage: true})
  await page.getByRole('link', {name: 'Plant 2', exact: true}).click()
  await expect(page).toHaveURL(`${base}/scenarios`)
  await expect(page.getByRole('link', {name: 'checkout-health', exact: true})).toBeVisible()
})
test('artifact reveal and download are authenticated and accessible', async ({page}) => {
  await page.goto(`${base}/artifacts`)
  await page.getByRole('link', {name: 'trace-1', exact: true}).click()
  await expect(page.getByText('sensitive trace', {exact: true})).toHaveCount(0)
  await page.getByRole('button', {name: 'Reveal sensitive content'}).click()
  await expect(page.getByText('sensitive trace', {exact: true})).toBeVisible()
  expect((await new AxeBuilder({page}).analyze()).violations).toEqual([])
})

test('project and account dead-letter details and confirmation dialogs are accessible', async ({page}) => {
  for (const scope of [base, `/a/${account}`]) {
    await page.goto(`${scope}/dead-letters/failure-1`)
    await expect(page.getByRole('heading', {name: 'failure-1'})).toBeVisible()
    expect((await new AxeBuilder({page}).analyze()).violations).toEqual([])
    await page.getByRole('button', {name: 'Resolve', exact: true}).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    expect((await new AxeBuilder({page}).analyze()).violations).toEqual([])
  }
})


test('worker block controls use readable fields and accessible versioned dialogs', async ({page}) => {
  const fingerprint = `sha256:${'a'.repeat(64)}`
  let version = 1
  let blockedWorkers: {identity: string; reason?: string}[] = []
  const runner = () => ({apiVersion: 'urth.sre-norns.com/v1', kind: 'runners', metadata: {uid: 'runner-1', name: 'edge', account, version}, spec: {active: true, blockedWorkers}})
  await page.route('**/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/v1/agent-identities/runner-1') return route.fulfill({json: identityResource('agent-identities', 'runner-1', {description: 'Edge runner'}, {}, {name: 'edge'})})
    if (path === '/v1/agent-identities/runner-1/tokens' || path === '/v1/agent-identities/runner-1/project-authorizations' || path === `/v1/accounts/${account}/workers`) return route.fulfill({json: pageOf([])})
    if (path !== `/v1/accounts/${account}/runners/edge`) return route.fallback()
    expect(route.request().headers()['authorization']).toBe('Bearer access')
    if (route.request().method() === 'PUT') {
      expect(route.request().headers()['if-match']).toBe(`"${version}"`)
      blockedWorkers = route.request().postDataJSON().spec.blockedWorkers
      version++
    }
    return route.fulfill({json: runner(), headers: {ETag: `"${version}"`}})
  })
  await page.goto(`/a/${account}/runners/runner-1`)
  await page.getByRole('button', {name: 'Block worker', exact: true}).click()
  let dialog = page.getByRole('dialog')
  await dialog.getByRole('textbox', {name: /Verified key fingerprint/}).fill(fingerprint)
  await dialog.getByRole('textbox', {name: 'Reason', exact: true}).fill('Retired installation')
  expect((await new AxeBuilder({page}).analyze()).violations).toEqual([])
  await dialog.getByRole('button', {name: 'Block worker', exact: true}).click()
  await expect(dialog).toHaveCount(0)
  await expect(page.getByText(fingerprint, {exact: true})).toBeVisible()
  expect(blockedWorkers).toEqual([{identity: fingerprint, reason: 'Retired installation'}])
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({path: test.info().outputPath('worker-blocklist.png'), fullPage: true})
  await page.getByRole('button', {name: 'Unblock', exact: true}).click()
  dialog = page.getByRole('dialog')
  await expect(dialog.getByText(fingerprint, {exact: true})).toBeVisible()
  expect((await new AxeBuilder({page}).analyze()).violations).toEqual([])
  await dialog.getByRole('button', {name: 'Unblock worker', exact: true}).click()
  await expect(dialog).toHaveCount(0)
  expect(blockedWorkers).toEqual([])
  expect(version).toBe(3)
})
