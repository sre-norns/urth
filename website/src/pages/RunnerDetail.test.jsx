import React from 'react'
import {describe, it, expect, vi} from 'vitest'
import {screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {renderWithProviders} from '../test/render.jsx'
import RunnerDetail from './RunnerDetail.jsx'
import {LabelWorker} from '../utils/labels.js'

vi.mock('../actions/fetchRunner.js', () => ({default: () => () => {}}))
vi.mock('../actions/fetchWorkers.js', () => ({default: () => () => {}}))

const setPaused = vi.fn(() => () => {})
const dropWorker = vi.fn(() => () => {})
vi.mock('../actions/setWorkerPaused.js', () => ({default: (...args) => setPaused(...args)}))
vi.mock('../actions/deleteWorker.js', () => ({default: (...args) => dropWorker(...args)}))

const runner = (overrides = {}) => ({
  kind: 'runners',
  metadata: {
    uid: 'runner-uid',
    version: 1,
    name: 'example-runner',
    labels: {os: 'linux'},
    creationTimestamp: '2026-07-21T00:00:00Z',
  },
  spec: {
    description: 'A runner resource',
    active: true,
    maxInstance: 0,
    requirements: {
      matchLabels: {os: 'linux'},
      matchSelector: [{key: 'env', operator: 'NotIn', values: ['dev', 'testing']}],
    },
    ...overrides,
  },
})

// A worker record. status defaults to nothing reported, which is what a record
// written before liveness reporting looks like -- `unknown`, not offline.
const worker = (name, status = {}) => ({
  kind: 'workerinstances',
  metadata: {
    uid: `uid-${name}`,
    version: 1,
    name,
    creationTimestamp: '2026-07-21T00:00:00Z',
    labels: {[LabelWorker.OS]: 'linux', [LabelWorker.Arch]: 'amd64'},
  },
  spec: {},
  status,
})

// online builds the status of a worker reporting on both paths.
const online = (extra = {}) => ({
  lastSeenTime: '2026-07-21T00:05:00Z',
  natsLastSeenTime: '2026-07-21T00:05:00Z',
  lastSeenVia: 'heartbeat',
  presence: {api: 'online', nats: 'online', condition: 'online'},
  ...extra,
})

const stateWith = (workers, runnerManifest = runner()) => ({
  scenario: {},
  scenarios: {},
  scenarioResults: {},
  scenarioActions: {},
  run: {},
  runArtifacts: {},
  artifactContent: {},
  runners: {},
  runner: {'example-runner': {fetching: false, response: runnerManifest}},
  workers: {'example-runner': {fetching: false, response: {data: workers}}},
})

const render = (state) => renderWithProviders(<RunnerDetail runnerId="example-runner" />, {preloadedState: state})

describe('RunnerDetail', () => {
  it('shows the runner identity, state and labels', () => {
    render(stateWith([]))

    expect(screen.getByText('example-runner')).toBeInTheDocument()
    expect(screen.getByText('A runner resource')).toBeInTheDocument()
    expect(screen.getByText('active')).toBeInTheDocument()
    expect(screen.getByText('os')).toBeInTheDocument()
  })

  // The requirements decide which workers may register at all, so they belong on
  // the page an operator opens when a worker is not showing up.
  it('lists the requirements a worker must satisfy', () => {
    render(stateWith([]))

    expect(screen.getByText('os = linux')).toBeInTheDocument()
    expect(screen.getByText('env NotIn (dev,testing)')).toBeInTheDocument()
  })

  it('counts the workers claiming this identity', () => {
    render(stateWith([worker('worker-01', online()), worker('worker-02', online({paused: true}))]))

    expect(screen.getByText('2 claiming this identity')).toBeInTheDocument()
    expect(screen.getByText('worker-01')).toBeInTheDocument()
    expect(screen.getByText('worker-02')).toBeInTheDocument()
  })

  it('links each worker name to its detail page', () => {
    render(stateWith([worker('worker-01', online())]))

    expect(screen.getByRole('link', {name: 'worker-01'})).toHaveAttribute('href', '/workers/worker-01')
  })

  // An operator's decision is the more important fact about a worker than its
  // liveness, so a paused worker reads as paused whatever its signals say.
  it('distinguishes a paused worker from one that is online', () => {
    render(stateWith([worker('worker-01', online()), worker('worker-02', online({paused: true}))]))

    expect(screen.getByText('online')).toBeInTheDocument()
    expect(screen.getByText('paused')).toBeInTheDocument()
    expect(screen.getByText('Pause')).toBeInTheDocument()
    expect(screen.getByText('Resume')).toBeInTheDocument()
  })

  it('pauses a single worker without touching the runner', async () => {
    setPaused.mockClear()
    render(stateWith([worker('worker-01', online())]))

    await userEvent.click(screen.getByText('Pause'))

    expect(setPaused).toHaveBeenCalledWith('worker-01', true, 'example-runner')
  })

  it('resumes a paused worker', async () => {
    setPaused.mockClear()
    render(stateWith([worker('worker-01', online({paused: true}))]))

    await userEvent.click(screen.getByText('Resume'))

    expect(setPaused).toHaveBeenCalledWith('worker-01', false, 'example-runner')
  })

  // The bug this whole feature exists for: a worker whose process is gone used
  // to render exactly like one that was running.
  it('shows a worker that stopped reporting as offline', () => {
    render(
      stateWith([
        worker('worker-01', {
          lastSeenTime: '2026-07-21T00:00:00Z',
          natsLastSeenTime: '2026-07-21T00:00:00Z',
          presence: {api: 'offline', nats: 'offline', condition: 'offline'},
        }),
      ])
    )

    expect(screen.getByText('offline')).toBeInTheDocument()
    expect(screen.getByText('not reporting on either path')).toBeInTheDocument()
    expect(screen.queryByText('online')).not.toBeInTheDocument()
  })

  // The two signals are reported separately so that a half-connected worker is
  // diagnosed rather than merely marked absent.
  it('names which path is broken when only one signal is lost', () => {
    render(
      stateWith([
        worker('on-nats-only', {
          natsLastSeenTime: '2026-07-21T00:05:00Z',
          presence: {api: 'offline', nats: 'online', condition: 'api-unreachable'},
        }),
        worker('on-api-only', {
          lastSeenTime: '2026-07-21T00:05:00Z',
          presence: {api: 'online', nats: 'offline', condition: 'nats-unreachable'},
        }),
      ])
    )

    expect(screen.getByText('no API contact')).toBeInTheDocument()
    expect(screen.getByText(/can be offered work and cannot claim it/)).toBeInTheDocument()

    expect(screen.getByText('not on its queue')).toBeInTheDocument()
    expect(screen.getByText(/nowhere to collect work from/)).toBeInTheDocument()
  })

  // A record from before liveness reporting, or a worker that does not report,
  // has told us nothing. Showing it as offline would be a different wrong answer
  // from the one this replaced, not a fix.
  it('reports a worker that has never reported as unknown rather than offline', () => {
    render(stateWith([worker('legacy-worker')]))

    expect(screen.getByText('presence unknown')).toBeInTheDocument()
    expect(screen.getByText(/never seen/)).toBeInTheDocument()
    expect(screen.queryByText('offline')).not.toBeInTheDocument()
  })

  // A claim is proof of life too, and saying which evidence it was distinguishes
  // "the process is up" from "the process is up and taking work".
  it('says when a worker was last seen, and on what evidence', () => {
    render(stateWith([worker('worker-01', online({lastSeenVia: 'claim'}))]))

    expect(screen.getByText(/last seen .*\(claimed a run\)/)).toBeInTheDocument()
  })

  it('counts how many workers are online, not merely registered', () => {
    render(
      stateWith([
        worker('worker-01', online()),
        worker('worker-02', {presence: {api: 'offline', nats: 'offline', condition: 'offline'}}),
        worker('worker-03'),
      ])
    )

    expect(screen.getByText('1/3')).toBeInTheDocument()
  })

  // Queue state comes from the broker and is shown only when the broker was
  // actually read: a "0 waiting" nobody asked for would read as authoritative.
  it('shows queue state when the transport could be observed', () => {
    render(stateWith([], {...runner(), status: {channel: {observed: true, pullers: 2, pending: 5}}}))

    expect(screen.getByText('Queue')).toBeInTheDocument()
    expect(screen.getByText('workers waiting · 5 job(s) queued')).toBeInTheDocument()
  })

  it('hides queue state entirely when the transport could not be observed', () => {
    render(stateWith([], {...runner(), status: {channel: {observed: false, pullers: 0, pending: 0}}}))

    expect(screen.queryByText('Queue')).not.toBeInTheDocument()
  })

  it('offers to disable an active runner', () => {
    render(stateWith([]))

    expect(screen.getByText('Disable runner')).toBeInTheDocument()
  })

  // A disabled runner takes no jobs and accepts no new registrations, so the
  // page has to say so rather than looking merely idle.
  it('shows a disabled runner as disabled and offers to enable it', () => {
    render(stateWith([], runner({active: false})))

    expect(screen.getByText('disabled')).toBeInTheDocument()
    expect(screen.getByText('workers cannot take jobs')).toBeInTheDocument()
    expect(screen.getByText('Enable runner')).toBeInTheDocument()
  })

  it('reports the worker limit when the runner sets one', () => {
    render(stateWith([worker('worker-01', online())], runner({maxInstance: 3})))

    expect(screen.getByText('1/3')).toBeInTheDocument()
    expect(screen.getByText('registered / limit')).toBeInTheDocument()
  })

  it('reports a load failure rather than rendering an empty page', () => {
    const state = stateWith([])
    state.runner = {'example-runner': {fetching: false, error: {message: 'boom'}}}

    render(state)

    expect(screen.getByText(/Error loading runner/)).toBeInTheDocument()
  })
})
