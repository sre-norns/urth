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

const worker = (name, paused = false) => ({
  kind: 'workerinstances',
  metadata: {
    uid: `uid-${name}`,
    version: 1,
    name,
    creationTimestamp: '2026-07-21T00:00:00Z',
    labels: {[LabelWorker.OS]: 'linux', [LabelWorker.Arch]: 'amd64'},
  },
  spec: {},
  status: {paused},
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
    render(stateWith([worker('worker-01'), worker('worker-02', true)]))

    expect(screen.getByText('2 claiming this identity')).toBeInTheDocument()
    expect(screen.getByText('worker-01')).toBeInTheDocument()
    expect(screen.getByText('worker-02')).toBeInTheDocument()
  })

  it('distinguishes a paused worker from one taking jobs', () => {
    render(stateWith([worker('worker-01'), worker('worker-02', true)]))

    expect(screen.getByText('taking jobs')).toBeInTheDocument()
    expect(screen.getByText('paused')).toBeInTheDocument()
    expect(screen.getByText('Pause')).toBeInTheDocument()
    expect(screen.getByText('Resume')).toBeInTheDocument()
  })

  it('pauses a single worker without touching the runner', async () => {
    setPaused.mockClear()
    render(stateWith([worker('worker-01')]))

    await userEvent.click(screen.getByText('Pause'))

    expect(setPaused).toHaveBeenCalledWith('worker-01', true, 'example-runner')
  })

  it('resumes a paused worker', async () => {
    setPaused.mockClear()
    render(stateWith([worker('worker-01', true)]))

    await userEvent.click(screen.getByText('Resume'))

    expect(setPaused).toHaveBeenCalledWith('worker-01', false, 'example-runner')
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
    render(stateWith([worker('worker-01')], runner({maxInstance: 3})))

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
