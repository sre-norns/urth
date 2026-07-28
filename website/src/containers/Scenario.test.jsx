import React from 'react'
import {describe, it, expect, vi} from 'vitest'
import {screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {renderWithProviders} from '../test/render.jsx'
import Scenario from './Scenario.jsx'

// The list item asks the server where each scenario would be placed. Left alone
// that thunk would reach the network, fail, and overwrite the state each test is
// trying to render.
vi.mock('../actions/fetchScenarioPlacement.js', () => ({default: () => () => {}}))

const scenario = ({results = [], active = true, schedule = '@5minutes', prob = {kind: 'http'}} = {}) => ({
  kind: 'scenarios',
  metadata: {
    uid: 'uid-1',
    name: 'checkout-probe',
    labels: {env: 'prod', team: 'checkout'},
  },
  spec: {
    active,
    description: 'Checks the checkout endpoint',
    schedule,
    prob,
  },
  status: {
    nextScheduledRunTime: '2026-07-21T10:00:00Z',
    results,
  },
})

const result = (status, resultValue) => ({
  metadata: {uid: 'run-1', name: 'run-1'},
  spec: {start_time: '2026-07-21T09:00:00Z'},
  status: {status, result: resultValue},
})

describe('Scenario list item', () => {
  it('renders the scenario name, schedule and labels', () => {
    renderWithProviders(<Scenario data={scenario()} />)

    expect(screen.getByText('checkout-probe')).toBeInTheDocument()
    expect(screen.getByText('@5minutes')).toBeInTheDocument()
    expect(screen.getByText('env')).toBeInTheDocument()
    expect(screen.getByText('prod')).toBeInTheDocument()
    expect(screen.getByText('team')).toBeInTheDocument()
  })

  it('reports a scenario with no runs as new', () => {
    renderWithProviders(<Scenario data={scenario()} />)

    expect(screen.getByText('new')).toBeInTheDocument()
  })

  it('reports the most recent run status', () => {
    renderWithProviders(<Scenario data={scenario({results: [result('completed', 'success')]})} />)

    expect(screen.getByText('completed/success')).toBeInTheDocument()
  })

  it('notifies when a label capsule is clicked, so the list can filter', async () => {
    const onCapsuleClick = vi.fn()
    renderWithProviders(<Scenario data={scenario()} onCapsuleClick={onCapsuleClick} />)

    await userEvent.click(screen.getByText(/env/))

    expect(onCapsuleClick).toHaveBeenCalled()
  })

  // A scenario with no prob body cannot be executed, and an inactive one should
  // not be triggerable by accident.
  it('disables the run control when the scenario is not runnable', () => {
    const {container} = renderWithProviders(<Scenario data={scenario({active: false})} />)

    const buttons = container.querySelectorAll('button')
    expect(buttons.length).toBeGreaterThan(0)
    expect(buttons[0]).toBeDisabled()
  })

  it('enables the run control for an active scenario with a prob', () => {
    const {container} = renderWithProviders(<Scenario data={scenario()} />)

    expect(container.querySelectorAll('button')[0]).not.toBeDisabled()
  })

  // The same gate as the scenario page. Triggering here produced a run that was
  // terminal the moment it existed -- the server refuses it correctly, but the
  // list should not be the one place that still offers it.
  it('disables the run control when no runner can take the scenario', () => {
    const {container} = renderWithProviders(<Scenario data={scenario()} />, {
      preloadedState: {
        scenarioPlacement: {
          'checkout-probe': {
            fetching: false,
            response: {
              requirements: 'os=linux',
              matchingRunners: 0,
              eligibleRunners: 0,
              registeredWorkers: 0,
              readyWorkers: 0,
              schedulable: false,
              reason: 'no-eligible-runner',
            },
          },
        },
      },
    })

    expect(container.querySelectorAll('button')[0]).toBeDisabled()
    // Why, without spending a line of the row on it.
    expect(screen.getByTitle(/No runner matches os=linux/)).toBeInTheDocument()
  })

  // A runner with no workers still queues the dispatch, so the run stays on
  // offer -- the same rule the scenario page follows.
  it('still offers a run when the matching runner has no workers', () => {
    const {container} = renderWithProviders(<Scenario data={scenario()} />, {
      preloadedState: {
        scenarioPlacement: {
          'checkout-probe': {
            fetching: false,
            response: {eligibleRunners: 1, matchingRunners: 1, readyWorkers: 0, schedulable: true},
          },
        },
      },
    })

    expect(container.querySelectorAll('button')[0]).not.toBeDisabled()
  })

  // Until the preview arrives the server remains the authority; guessing would
  // disable every row on first paint.
  it('offers the run while the placement preview is still loading', () => {
    const {container} = renderWithProviders(<Scenario data={scenario()} />, {
      preloadedState: {scenarioPlacement: {'checkout-probe': {fetching: true}}},
    })

    expect(container.querySelectorAll('button')[0]).not.toBeDisabled()
  })
})
