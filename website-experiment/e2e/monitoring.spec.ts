import {expect, test} from '@playwright/test'

const scenario = {
  kind: 'scenarios',
  metadata: {name: 'checkout-health', uid: 'scenario-1', labels: {team: 'checkout'}},
  spec: {active: true, description: 'Checkout service health', schedule: '@5minutes', prob: {kind: 'http'}},
  status: {
    results: [{name: 'run-1', uid: 'run-1', spec: {start_time: '2026-07-29T00:00:00Z', end_time: '2026-07-29T00:00:01Z'}, status: {status: 'completed', result: 'success'}}],
  },
}

test.beforeEach(async ({page}) => {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    if (url.pathname === '/api/v1/scenarios') return route.fulfill({json: {data: [scenario], total: 1}})
    if (url.pathname === '/api/v1/scenarios/checkout-health') return route.fulfill({json: scenario})
    if (url.pathname.endsWith('/placement')) return route.fulfill({json: {schedulable: true, eligibleRunners: 1, readyWorkers: 2}})
    if (url.pathname.endsWith('/results')) return route.fulfill({json: {data: scenario.status.results, total: 1}})
    return route.fulfill({json: {data: [], total: 0}})
  })
})

test('moves from the scenario fleet to operational detail', async ({page}) => {
  await page.goto('/scenarios')
  await expect(page.getByRole('heading', {name: 'Scenarios'})).toBeVisible()
  await page.getByRole('link', {name: 'checkout-health'}).click()
  await expect(page.getByRole('heading', {name: /checkout-health/})).toBeVisible()
  await expect(page.getByRole('button', {name: 'Run now'})).toBeEnabled()
})
