import React from 'react'
import {describe, it, expect, vi} from 'vitest'
import {screen} from '@testing-library/react'
import {renderWithProviders} from '../test/render.jsx'
import ScenarioDetail from './ScenarioDetail.jsx'

// The page loads itself on mount. Left alone those thunks would reach the
// network, fail, and overwrite the state each test is trying to render.
vi.mock('../actions/fetchScenario.js', () => ({default: () => () => {}}))
vi.mock('../actions/fetchScenarioResults.js', () => ({default: () => () => {}}))
vi.mock('../actions/fetchScenarioPlacement.js', () => ({default: () => () => {}}))
vi.mock('../actions/runScenario.js', () => ({default: () => () => {}}))

const scenario = {
  kind: 'scenarios',
  metadata: {uid: 'uid-1', name: 'checkout-probe', labels: {env: 'prod'}},
  spec: {
    active: true,
    description: 'Checks the checkout endpoint',
    schedule: '@5minutes',
    prob: {kind: 'http'},
  },
  status: {nextScheduledRunTime: '2099-01-01T00:00:00Z', results: []},
}

const run = (name, result, startISO, endISO) => ({
  uid: `uid-${name}`,
  name,
  spec: {probKind: 'http', start_time: startISO, end_time: endISO},
  status: {status: 'completed', result, numberArtifacts: 2},
})

// A fleet that can take this scenario's runs. Overridden per test for the cases
// where it cannot.
const placement = {
  requirements: 'os=linux',
  matchingRunners: 2,
  eligibleRunners: 2,
  registeredWorkers: 3,
  readyWorkers: 3,
  schedulable: true,
}

const stateWith = (runs, overrides = {}, preview = placement) => ({
  scenario: {id: 'checkout-probe', fetching: false, response: scenario, ...overrides},
  scenarioResults: {'checkout-probe': {fetching: false, response: {data: runs}}},
  scenarioPlacement: preview ? {'checkout-probe': {fetching: false, response: preview}} : {},
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
    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, {preloadedState: stateWith([])})

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

    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, {preloadedState: stateWith(runs)})

    expect(screen.getByText('Success rate')).toBeInTheDocument()
    expect(screen.getByText('67%')).toBeInTheDocument()
    expect(screen.getByText('2 of 3 settled')).toBeInTheDocument()
  })

  it('lists the run history with outcomes and durations', () => {
    const runs = [run('r1', 'success', ...recent(5, 1500))]

    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, {preloadedState: stateWith(runs)})

    expect(screen.getByText('success')).toBeInTheDocument()
    expect(screen.getByText('2 artifacts')).toBeInTheDocument()
    // The duration shows twice with a single run: once as the average, once on
    // the run itself.
    expect(screen.getAllByText('1.5s')).toHaveLength(2)
  })

  it('offers a manual run for a runnable scenario', () => {
    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, {preloadedState: stateWith([])})

    expect(screen.getByText(/Run now/)).toBeInTheDocument()
  })

  // A scenario with no prob body cannot execute; saying so is more useful than
  // a button that fails.
  it('explains why a scenario without a prob cannot run', () => {
    const state = stateWith([])
    state.scenario.response = {...scenario, spec: {...scenario.spec, prob: undefined}}

    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, {preloadedState: state})

    expect(screen.getByText(/no prob defined/)).toBeInTheDocument()
  })

  // The whole point of the preview: a scenario nothing can take produces a run
  // that is terminal the moment it is created, so the button is taken away and
  // the requirement that matched nothing is named.
  it('refuses a manual run when no runner matches, and says what is missing', () => {
    const state = stateWith([], {}, {
      requirements: 'os=linux,env notin (dev)',
      matchingRunners: 0,
      eligibleRunners: 0,
      registeredWorkers: 0,
      readyWorkers: 0,
      schedulable: false,
      reason: 'no-eligible-runner',
    })

    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, {preloadedState: state})

    expect(screen.getByText(/Run now/).closest('button')).toBeDisabled()
    expect(screen.getByText(/No runner matches os=linux,env notin \(dev\)/)).toBeInTheDocument()
  })

  // Matching runners that are all disabled needs a different action than no
  // runners at all, so it gets a different sentence.
  it('distinguishes matching runners that are all disabled', () => {
    const state = stateWith([], {}, {
      requirements: 'os=linux',
      matchingRunners: 2,
      eligibleRunners: 0,
      registeredWorkers: 0,
      readyWorkers: 0,
      schedulable: false,
      reason: 'no-eligible-runner',
    })

    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, {preloadedState: state})

    expect(screen.getByText(/2 runners match os=linux, but none is active/)).toBeInTheDocument()
  })

  // A runner with no workers is not a refusal: the dispatch is durable and waits
  // in the queue, so the run stays on offer with a warning.
  it('still offers a run when the matching runner has no workers', () => {
    const state = stateWith([], {}, {...placement, eligibleRunners: 1, registeredWorkers: 0, readyWorkers: 0})

    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, {preloadedState: state})

    expect(screen.getByText(/Run now/).closest('button')).not.toBeDisabled()
    expect(screen.getByText(/a run will queue until one connects/)).toBeInTheDocument()
  })

  it('shows how much capacity the scenario has', () => {
    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, {preloadedState: stateWith([])})

    expect(screen.getByText('Capacity')).toBeInTheDocument()
    expect(screen.getByText('2 / 3')).toBeInTheDocument()
    expect(screen.getByText('eligible runners / ready workers')).toBeInTheDocument()
  })

  // Until the preview arrives the server remains the authority. Guessing
  // "unschedulable" from a missing answer would disable the button on every
  // first paint.
  it('offers the run while the placement preview is still loading', () => {
    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, {preloadedState: stateWith([], {}, null)})

    expect(screen.getByText(/Run now/).closest('button')).not.toBeDisabled()
  })

  it('reports an error rather than rendering an empty page', () => {
    const state = stateWith([])
    state.scenario = {id: 'checkout-probe', fetching: false, error: {message: 'boom'}}

    renderWithProviders(<ScenarioDetail scenarioId="checkout-probe" />, {preloadedState: state})

    expect(screen.getByText(/Error loading scenario/)).toBeInTheDocument()
  })
})
