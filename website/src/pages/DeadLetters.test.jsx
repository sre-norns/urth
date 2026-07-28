import React from 'react'
import {describe, it, expect, vi, beforeEach} from 'vitest'
import {screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {renderWithProviders} from '../test/render.jsx'
import DeadLetters from './DeadLetters.jsx'

const fetchDeadLetters = vi.fn(() => () => {})
const retryDeadLetter = vi.fn(() => () => {})
const resolveDeadLetter = vi.fn(() => () => {})

vi.mock('../actions/fetchDeadLetters.js', () => ({
  default: (...args) => fetchDeadLetters(...args),
  retryDeadLetter: (...args) => retryDeadLetter(...args),
  resolveDeadLetter: (...args) => resolveDeadLetter(...args),
}))

const failure = (name, overrides = {}) => ({
  kind: 'dispatchFailures',
  metadata: {
    uid: `uid-${name}`,
    version: 1,
    name,
    creationTimestamp: '2026-07-28T00:00:00Z',
    labels: {'urth/runner.name': 'example-runner', ...(overrides.labels || {})},
  },
  spec: {
    reason: 'policy-refused',
    eventUID: `${name}.1`,
    scenarioName: 'my-probe',
    reportedBy: 'worker',
    detail: 'the API permanently refused this worker claim',
    ...(overrides.spec || {}),
  },
  status: {resolved: false, ...(overrides.status || {})},
})

const stateWith = (data, extra = {}) => ({
  deadLetters: {fetching: false, response: {data}, acting: {}, ...extra},
})

const render = (state, path = '/dead-letters') =>
  renderWithProviders(<DeadLetters />, {preloadedState: state, path})

describe('DeadLetters', () => {
  beforeEach(() => {
    fetchDeadLetters.mockClear()
    retryDeadLetter.mockClear()
    resolveDeadLetter.mockClear()
  })

  it('shows what failed, why, and where', () => {
    render(stateWith([failure('dispatch-1')]))

    expect(screen.getByText('dispatch-1')).toBeInTheDocument()
    expect(screen.getByText('policy-refused')).toBeInTheDocument()
    expect(screen.getByText('my-probe')).toBeInTheDocument()
    expect(screen.getByText('example-runner')).toBeInTheDocument()
    expect(screen.getByText(/permanently refused/)).toBeInTheDocument()
  })

  // "nothing outstanding" and "nothing matched" render as the same empty table,
  // and only one of them is good news.
  it('says so explicitly when nothing is outstanding', () => {
    render(stateWith([]))

    expect(screen.getByText('No unresolved dispatch failures.')).toBeInTheDocument()
  })

  it('offers retry and resolve on an unresolved failure', async () => {
    render(stateWith([failure('dispatch-1')]))

    await userEvent.click(screen.getByRole('button', {name: 'Retry'}))
    expect(retryDeadLetter).toHaveBeenCalledWith('dispatch-1')

    await userEvent.click(screen.getByRole('button', {name: 'Resolve'}))
    expect(resolveDeadLetter).toHaveBeenCalledWith('dispatch-1')
  })

  // A retry creates a new run and never reopens the failed one, so the record
  // has to point at the re-attempt rather than offering to retry it again.
  it('links to the retry instead of offering another one', () => {
    render(stateWith([failure('dispatch-1', {status: {resolved: true, retryResultName: 'run-abc'}})]))

    expect(screen.getByText('retried')).toBeInTheDocument()
    expect(screen.queryByRole('button', {name: 'Retry'})).not.toBeInTheDocument()
  })

  it('shows a resolved failure as closed without a retry link', () => {
    render(stateWith([failure('dispatch-1', {status: {resolved: true}})]))

    expect(screen.getByText('resolved')).toBeInTheDocument()
    expect(screen.queryByRole('button', {name: 'Retry'})).not.toBeInTheDocument()
  })

  // Two operators, or two clicks, must not schedule two runs. The button is
  // disabled per row rather than for the whole table.
  it('disables the actions for a row while one is in flight', () => {
    render(stateWith([failure('dispatch-1'), failure('dispatch-2')], {acting: {'dispatch-1': true}}))

    const retries = screen.getAllByRole('button', {name: 'Retry'})
    expect(retries[0]).toBeDisabled()
    expect(retries[1]).not.toBeDisabled()
  })

  it('names the run a retry created', () => {
    render(
      stateWith([failure('dispatch-1')], {
        lastAction: {name: 'dispatch-1', response: {retry: {metadata: {name: 'run-abc'}}}},
      })
    )

    expect(screen.getByText('run-abc')).toBeInTheDocument()
  })

  it('reports a failed action rather than silently doing nothing', () => {
    render(stateWith([failure('dispatch-1')], {actionError: new Error('the run no longer exists')}))

    expect(screen.getByText(/the run no longer exists/)).toBeInTheDocument()
  })

  it('surfaces a fetch error', () => {
    render({deadLetters: {fetching: false, error: new Error('boom'), acting: {}}})

    expect(screen.getByText(/Error fetching dispatch failures/)).toBeInTheDocument()
  })
})
