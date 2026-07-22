import React from 'react'
import {describe, it, expect, vi} from 'vitest'
import {screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {renderWithProviders} from '../test/render.jsx'
import RunResult from './RunResult.jsx'

const run = (overrides = {}) => ({
  uid: 'run-uid',
  name: 'kbjk96thvzcuiass',
  labels: {
    'urth/scenario.name': 'tcp-self-fondle',
    'urth/worker.name': 'worker-01',
    'urth/result.result': 'success',
    'urth/result.uid': 'noise-that-repeats-the-row',
    'run.messageId': 'more-noise',
    team: 'checkout',
  },
  spec: {
    probKind: 'tcp',
    start_time: '2026-07-21T10:00:00+10:00',
    end_time: '2026-07-21T10:00:01.500+10:00',
  },
  status: {status: 'completed', result: 'success', numberArtifacts: 2},
  ...overrides,
})

describe('RunResult row', () => {
  it('leads with the scenario, since a run name means little on its own', () => {
    renderWithProviders(<RunResult data={run()} />)

    expect(screen.getByText('tcp-self-fondle')).toBeInTheDocument()
    expect(screen.getByText(/kbjk96thvzcuiass/)).toBeInTheDocument()
  })

  it('shows outcome, duration, worker and artifact count', () => {
    renderWithProviders(<RunResult data={run()} />)

    expect(screen.getByText('success')).toBeInTheDocument()
    expect(screen.getByText('1.5s')).toBeInTheDocument()
    expect(screen.getByText('worker-01')).toBeInTheDocument()
    expect(screen.getByText('2 artifacts')).toBeInTheDocument()
  })

  it('links to the run, which is reachable without its scenario', () => {
    renderWithProviders(<RunResult data={run()} />)

    expect(screen.getByText('tcp-self-fondle').closest('a')).toHaveAttribute('href', '/results/kbjk96thvzcuiass')
  })

  // Every run carries a dozen system labels that restate what the row already
  // shows. Rendering them all would bury the ones that mean something.
  it('hides system labels that repeat the row', () => {
    renderWithProviders(<RunResult data={run()} />)

    expect(screen.getByText('team')).toBeInTheDocument()
    expect(screen.queryByText('noise-that-repeats-the-row')).not.toBeInTheDocument()
    expect(screen.queryByText('more-noise')).not.toBeInTheDocument()
  })

  it('narrows the list when a label is clicked', async () => {
    const onCapsuleClick = vi.fn()
    renderWithProviders(<RunResult data={run()} onCapsuleClick={onCapsuleClick} />)

    await userEvent.click(screen.getByText('team'))

    expect(onCapsuleClick).toHaveBeenCalled()
  })

  // A run that has been scheduled but not yet claimed has no outcome and no
  // duration; it should read as pending rather than as a zero-length failure.
  it('renders a run that has not finished', () => {
    const pending = run({spec: {}, status: {status: 'pending'}})

    renderWithProviders(<RunResult data={pending} />)

    expect(screen.getByText('pending')).toBeInTheDocument()
    // No duration: a run that has not finished is not a zero-length run.
    expect(screen.getAllByText('—').length).toBeGreaterThan(0)
  })
})
