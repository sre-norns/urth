import React from 'react'
import {act, screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {renderWithProviders} from '../test/render.jsx'
import {LabelRunner, LabelScenario, LabelWorker} from '../utils/labels.js'
import {RESOURCE_REFRESH_INTERVAL_MS} from '../utils/refresh.js'
import {formatTimestamp} from '../utils/time.js'
import WorkerDetail from './WorkerDetail.jsx'

const fetchWorker = vi.fn(() => () => {})
const fetchRuns = vi.fn(() => () => {})
const setPaused = vi.fn(() => () => {})

vi.mock('../actions/fetchWorker.js', () => ({default: (...args) => fetchWorker(...args)}))
vi.mock('../actions/fetchWorkerResults.js', async (importOriginal) => {
  const actual = await importOriginal()
  return {...actual, default: (...args) => fetchRuns(...args)}
})
vi.mock('../actions/setWorkerPaused.js', () => ({default: (...args) => setPaused(...args)}))

const worker = (status = {}) => ({
  kind: 'workerinstances',
  metadata: {
    uid: 'worker-uid',
    version: 3,
    name: 'agent.build-7',
    creationTimestamp: '2026-07-21T00:00:00Z',
    labels: {
      [LabelRunner.Name]: 'example-runner',
      [LabelRunner.UID]: 'runner-uid',
      [LabelWorker.Hostname]: 'build-7',
      [LabelWorker.OS]: 'linux',
      [LabelWorker.Arch]: 'amd64',
      [LabelWorker.BuildVersion]: 'v1.2.3',
      'urth/capability.prob.http': 'v1',
      team: 'checkout',
    },
  },
  spec: {},
  status,
})

const run = {
  uid: 'run-uid',
  name: 'run-01',
  creationTimestamp: '2026-07-21T00:06:00Z',
  labels: {
    [LabelScenario.Name]: 'checkout-probe',
    [LabelWorker.Name]: 'agent.build-7',
  },
  spec: {
    start_time: '2026-07-21T00:06:00Z',
    end_time: '2026-07-21T00:06:02Z',
  },
  status: {status: 'completed', result: 'success', numberArtifacts: 1},
}

const online = (extra = {}) => ({
  lastSeenTime: '2026-07-21T00:05:00Z',
  lastSeenVia: 'heartbeat',
  natsLastSeenTime: '2026-07-21T00:05:30Z',
  presence: {api: 'online', nats: 'online', condition: 'online'},
  ...extra,
})

const stateWith = ({manifest = worker(online()), runs = [run], total = runs.length, workerError, runError} = {}) => ({
  worker: {
    'agent.build-7': {
      fetching: false,
      response: manifest,
      error: workerError,
    },
  },
  workerResults: {
    'agent.build-7': {
      fetching: false,
      response: runError ? undefined : {data: runs, total},
      error: runError,
    },
  },
})

const render = (state = stateWith()) =>
  renderWithProviders(<WorkerDetail workerName="agent.build-7" />, {
    preloadedState: state,
    path: '/workers/agent.build-7',
  })

beforeEach(() => {
  fetchWorker.mockClear()
  fetchRuns.mockClear()
  setPaused.mockClear()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('WorkerDetail', () => {
  it('shows identity, membership, labels and the complete run count', () => {
    render(stateWith({total: 42}))

    expect(screen.getByRole('heading', {name: 'agent.build-7'})).toBeInTheDocument()
    expect(screen.getAllByText('build-7').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('linux/amd64')).toBeInTheDocument()
    expect(screen.getAllByText('v1.2.3').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('worker-uid')).toBeInTheDocument()
    expect(screen.getByText('42')).toBeInTheDocument()
    expect(screen.getByText('urth/capability.prob.http')).toBeInTheDocument()
    expect(screen.getByText('team')).toBeInTheDocument()

    expect(screen.getByRole('link', {name: '← Runner example-runner'})).toHaveAttribute(
      'href',
      '/runners/example-runner'
    )
    expect(screen.getByRole('link', {name: 'checkout-probe'})).toHaveAttribute('href', '/results/run-01')
    expect(screen.getByText('Showing 1 of 42 jobs')).toBeInTheDocument()
  })

  it('places recent runs on the page background so odd-row shading remains visible', () => {
    render()

    expect(getComputedStyle(screen.getByRole('region', {name: 'Recent runs'})).backgroundColor).toBe('rgba(0, 0, 0, 0)')
  })

  it('shows each online signal with its timestamp and API evidence', () => {
    render()

    expect(screen.getAllByText('online · fresh')).toHaveLength(2)
    expect(screen.getByText('Evidence: heartbeat')).toBeInTheDocument()
    expect(screen.getByText(formatTimestamp('2026-07-21T00:05:00Z'))).toBeInTheDocument()
    expect(screen.getByText(formatTimestamp('2026-07-21T00:05:30Z'))).toBeInTheDocument()
    expect(screen.getByText('Verdict: online')).toBeInTheDocument()
  })

  it('shows both paths stale when the worker is offline', () => {
    render(
      stateWith({
        manifest: worker({
          lastSeenTime: '2026-07-20T00:00:00Z',
          natsLastSeenTime: '2026-07-20T00:00:00Z',
          presence: {api: 'offline', nats: 'offline', condition: 'offline'},
        }),
      })
    )

    expect(screen.getAllByText('offline · stale')).toHaveLength(2)
    expect(screen.getByText('Verdict: offline')).toBeInTheDocument()
    expect(screen.getByText('not reporting on either path')).toBeInTheDocument()
  })

  it('diagnoses a split connection from the server verdict and signal states', () => {
    render(
      stateWith({
        manifest: worker({
          lastSeenTime: '2026-07-20T00:00:00Z',
          lastSeenVia: 'claim',
          natsLastSeenTime: '2026-07-21T00:05:30Z',
          presence: {api: 'offline', nats: 'online', condition: 'api-unreachable'},
        }),
      })
    )

    expect(screen.getByText('offline · stale')).toBeInTheDocument()
    expect(screen.getByText('online · fresh')).toBeInTheDocument()
    expect(screen.getByText('Evidence: claim — this worker took a run')).toBeInTheDocument()
    expect(screen.getByText('Verdict: no API contact')).toBeInTheDocument()
    expect(screen.getByText(/can be offered work and cannot claim it/)).toBeInTheDocument()
  })

  it('names NATS as the broken path when API contact remains fresh', () => {
    render(
      stateWith({
        manifest: worker({
          lastSeenTime: '2026-07-21T00:05:00Z',
          lastSeenVia: 'heartbeat',
          natsLastSeenTime: '2026-07-20T00:00:00Z',
          presence: {api: 'online', nats: 'offline', condition: 'nats-unreachable'},
        }),
      })
    )

    expect(screen.getByText('online · fresh')).toBeInTheDocument()
    expect(screen.getByText('offline · stale')).toBeInTheDocument()
    expect(screen.getByText('Verdict: not on its queue')).toBeInTheDocument()
    expect(screen.getByText(/nowhere to collect work from/)).toBeInTheDocument()
  })

  it('reports a never-seen worker as unknown and shows a recorded departure', () => {
    render(
      stateWith({
        manifest: worker({
          leftAt: '2026-07-21T00:04:00Z',
          presence: {api: 'unknown', nats: 'unknown', condition: 'unknown'},
        }),
      })
    )

    expect(screen.getAllByText('unknown · not observed')).toHaveLength(2)
    expect(screen.getAllByText('Never observed')).toHaveLength(2)
    expect(screen.getByText('Verdict: presence unknown')).toBeInTheDocument()
    expect(screen.getByText(/Announced/)).toBeInTheDocument()
    expect(screen.getByText(formatTimestamp('2026-07-21T00:04:00Z'))).toBeInTheDocument()
  })

  it('uses unknown when an older registration has no hostname label', () => {
    const manifest = worker(online())
    delete manifest.metadata.labels[LabelWorker.Hostname]

    render(stateWith({manifest}))

    expect(screen.getByText('Host')).toBeInTheDocument()
    expect(screen.getByText('unknown')).toBeInTheDocument()
  })

  it('pauses and refreshes the current worker without exposing Drop', async () => {
    render()

    expect(screen.queryByText('Drop')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Pause worker'}))

    expect(setPaused).toHaveBeenCalledWith('agent.build-7', true, 'example-runner')
    expect(fetchWorker).toHaveBeenCalledTimes(2)
    expect(fetchRuns).toHaveBeenCalledTimes(2)
  })

  it('resumes a paused worker', async () => {
    render(stateWith({manifest: worker(online({paused: true}))}))

    await userEvent.click(screen.getByRole('button', {name: 'Resume worker'}))

    expect(setPaused).toHaveBeenCalledWith('agent.build-7', false, 'example-runner')
  })

  it('polls both resources while mounted and stops after unmount', () => {
    vi.useFakeTimers()
    const view = render()

    expect(fetchWorker).toHaveBeenCalledTimes(1)
    expect(fetchRuns).toHaveBeenCalledTimes(1)

    act(() => vi.advanceTimersByTime(RESOURCE_REFRESH_INTERVAL_MS))

    expect(fetchWorker).toHaveBeenCalledTimes(2)
    expect(fetchRuns).toHaveBeenCalledTimes(2)

    view.unmount()
    act(() => vi.advanceTimersByTime(RESOURCE_REFRESH_INTERVAL_MS))

    expect(fetchWorker).toHaveBeenCalledTimes(2)
    expect(fetchRuns).toHaveBeenCalledTimes(2)
  })

  it('keeps worker diagnostics visible when run history fails', () => {
    render(stateWith({runError: {message: 'results unavailable'}}))

    expect(screen.getByRole('heading', {name: 'agent.build-7'})).toBeInTheDocument()
    expect(screen.getByText(/Error loading this worker.s runs/)).toBeInTheDocument()
    expect(screen.getByText('results unavailable')).toBeInTheDocument()
  })
})
