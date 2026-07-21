import React from 'react'
import { describe, it, expect, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { renderWithProviders } from '../test/render.jsx'
import ScenarioDetail from './ScenarioDetail.jsx'

// The page loads itself on mount. Left alone those thunks would reach the
// network, fail, and overwrite the state each test is trying to render.
vi.mock('../actions/fetchScenario.js', () => ({ default: () => () => {} }))
vi.mock('../actions/fetchScenarioResults.js', () => ({ default: () => () => {} }))
vi.mock('../actions/runScenario.js', () => ({ default: () => () => {} }))

const scenario = {
  kind: 'scenarios',
  metadata: { uid: 'uid-1', name: 'checkout-probe', labels: { env: 'prod' } },
  spec: {
    active: true,
    description: 'Checks the checkout endpoint',
    schedule: '@5minutes',
    prob: { kind: 'http' },
  },
  status: { nextScheduledRunTime: '2099-01-01T00:00:00Z', results: [] },
}

const run = (name, result, startISO, endISO) => ({
  uid: `uid-${name}`,
  name,
  spec: { probKind: 'http', start_time: startISO, end_time: endISO },
  status: { status: 'completed', result, numberArtifacts: 2 },
})

const stateWith = (runs, overrides = {}) => ({
  scenario: { id: 'checkout-probe', fetching: false, response: scenario, ...overrides },
  scenarioResults: { 'checkout-probe': { fetching: false, response: { data: runs } } },
  scenarioActions: {},
  scenarios: {},
  run: {},
  runArtifacts: {},
  artifactContent: {},
})

const recent = (minutesAgo, durationMs = 1000) => {
  const start = new Date(Date.now() - minutesAgo * 60_000)
  return [start.toISOString(), new Date(start.getTime() + durationMs).toISOString()]
}

describe('ScenarioDetail', () => {
  it('shows the scenario identity, type and schedule', () => {
    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, { preloadedState: stateWith([]) })

    expect(screen.getByText('checkout-probe')).toBeInTheDocument()
    expect(screen.getByText('Checks the checkout endpoint')).toBeInTheDocument()
    expect(screen.getByText('@5minutes')).toBeInTheDocument()
    expect(screen.getByText('http')).toBeInTheDocument()
    expect(screen.getByText('active')).toBeInTheDocument()
  })

  it('summarises runs over the selected period', () => {
    const runs = [
      run('r1', 'success', ...recent(10)),
      run('r2', 'success', ...recent(20)),
      run('r3', 'failed', ...recent(30)),
    ]

    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, { preloadedState: stateWith(runs) })

    expect(screen.getByText('Success rate')).toBeInTheDocument()
    expect(screen.getByText('67%')).toBeInTheDocument()
    expect(screen.getByText('2 of 3 settled')).toBeInTheDocument()
  })

  it('lists the run history with outcomes and durations', () => {
    const runs = [run('r1', 'success', ...recent(5, 1500))]

    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, { preloadedState: stateWith(runs) })

    expect(screen.getByText('success')).toBeInTheDocument()
    expect(screen.getByText('2 artifacts')).toBeInTheDocument()
    // The duration shows twice with a single run: once as the average, once on
    // the run itself.
    expect(screen.getAllByText('1.5s')).toHaveLength(2)
  })

  it('offers a manual run for a runnable scenario', () => {
    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, { preloadedState: stateWith([]) })

    expect(screen.getByText(/Run now/)).toBeInTheDocument()
  })

  // A scenario with no prob body cannot execute; saying so is more useful than
  // a button that fails.
  it('explains why a scenario without a prob cannot run', () => {
    const state = stateWith([])
    state.scenario.response = { ...scenario, spec: { ...scenario.spec, prob: undefined } }

    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, { preloadedState: state })

    expect(screen.getByText(/no prob defined/)).toBeInTheDocument()
  })

  it('reports an error rather than rendering an empty page', () => {
    const state = stateWith([])
    state.scenario = { id: 'checkout-probe', fetching: false, error: { message: 'boom' } }

    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, { preloadedState: state })

    expect(screen.getByText(/Error loading scenario/)).toBeInTheDocument()
  })
})
