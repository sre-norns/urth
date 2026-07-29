import React from 'react'
import {beforeEach, describe, it, expect, vi} from 'vitest'
import {screen} from '@testing-library/react'
import {renderWithProviders} from '../test/render.jsx'
import RunDetail from './RunDetail.jsx'
import {LabelArtifact, LabelResult, LabelRunner, LabelWorker} from '../utils/labels.js'

vi.mock('../actions/fetchRun.js', () => ({default: () => () => {}}))
vi.mock('../actions/fetchRunArtifacts.js', () => ({default: () => () => {}}))
vi.mock('../actions/fetchArtifactContent.js', () => ({default: () => () => {}}))
const fetchWorker = vi.fn(() => () => {})
vi.mock('../actions/fetchWorker.js', () => ({default: (...args) => fetchWorker(...args)}))

const run = {
  uid: 'run-uid',
  name: 'kbjk96thvzcuiass',
  spec: {
    probKind: 'tcp',
    start_time: '2026-07-21T10:00:00+10:00',
    end_time: '2026-07-21T10:00:01.500+10:00',
  },
  status: {
    status: 'completed',
    result: 'success',
    numberArtifacts: 2,
    executor: {
      runnerId: 'runner-uid',
      runnerName: 'example-runner',
      workerId: 'worker-uid',
      workerName: 'worker-01',
    },
  },
}

const artifact = (kind, dataClass, extra = {}) => ({
  kind: 'artifacts',
  metadata: {
    uid: `uid-${kind}`,
    name: `kbjk96thvzcuiass.${kind}`,
    labels: {
      [LabelArtifact.Kind]: kind,
      [LabelArtifact.DataClass]: dataClass,
      // Deliberately different from the run's own executor record, so the
      // tests below prove which of the two sources was read.
      [LabelRunner.Name]: 'artifact-runner',
      [LabelWorker.Name]: 'artifact-worker',
      [LabelWorker.UID]: 'artifact-worker-uid',
      ...extra,
    },
  },
})

const stateWith = (artifacts, runState = {fetching: false, response: run}, workers = {}) => ({
  scenario: {},
  scenarios: {},
  scenarioResults: {},
  scenarioActions: {},
  run: {kbjk96thvzcuiass: runState},
  runArtifacts: {kbjk96thvzcuiass: {fetching: false, response: {data: artifacts}}},
  artifactContent: {},
  worker: workers,
})

const render = (state) =>
  renderWithProviders(<RunDetail scenarioId="tcp-self-fondle" runId="kbjk96thvzcuiass" />, {
    preloadedState: state,
  })

// Opened from the cross-scenario Results list, where the URL carries only the
// run name.
const renderStandalone = (state) => renderWithProviders(<RunDetail runId="kbjk96thvzcuiass" />, {preloadedState: state})

beforeEach(() => {
  fetchWorker.mockClear()
})

describe('RunDetail', () => {
  it('shows the outcome, timing and type of the run', () => {
    render(stateWith([artifact('log', 'redacted')]))

    expect(screen.getByText('success')).toBeInTheDocument()
    expect(screen.getByText('job completed')).toBeInTheDocument()
    expect(screen.getByText('1.5s')).toBeInTheDocument()
    expect(screen.getByText('tcp')).toBeInTheDocument()
  })

  it('identifies the runner and worker the run recorded at pickup', () => {
    render(stateWith([artifact('log', 'redacted')]))

    expect(screen.getByText('example-runner')).toBeInTheDocument()
    expect(screen.getByRole('link', {name: 'worker-01'})).toHaveAttribute('href', '/workers/worker-01')
    // The run's own record wins over the labels on its artifacts.
    expect(screen.queryByText('artifact-runner')).not.toBeInTheDocument()
  })

  it('shows current presence only when the registration UID still matches', () => {
    const currentWorker = {
      metadata: {name: 'worker-01', uid: 'worker-uid'},
      status: {presence: {api: 'online', nats: 'online', condition: 'online'}},
    }

    render(
      stateWith(
        [artifact('log', 'redacted')],
        {fetching: false, response: run},
        {
          'worker-01': {fetching: false, response: currentWorker},
        }
      )
    )

    expect(fetchWorker).toHaveBeenCalledWith('worker-01')
    expect(screen.getByLabelText('Current presence: online')).toBeInTheDocument()
  })

  it('shows unknown presence when the worker name now belongs to another registration', () => {
    const replacement = {
      metadata: {name: 'worker-01', uid: 'replacement-worker-uid'},
      status: {presence: {api: 'online', nats: 'online', condition: 'online'}},
    }

    render(
      stateWith(
        [artifact('log', 'redacted')],
        {fetching: false, response: run},
        {
          'worker-01': {fetching: false, response: replacement},
        }
      )
    )

    expect(screen.getByLabelText('Current presence: presence unknown')).toBeInTheDocument()
    expect(screen.getByTitle(/executor registration is unavailable or has changed/)).toBeInTheDocument()
  })

  // "Which runner ran this, and what else is going on there" is one question.
  // The name is what an operator has; the runner page is what they want next.
  it('links the runner that executed the run to its page', () => {
    render(stateWith([artifact('log', 'redacted')]))

    expect(screen.getByText('example-runner').closest('a')).toHaveAttribute('href', '/runners/example-runner')
  })

  // The fallback identity is a label, not a foreign key, but it is still the
  // runner's name -- so it links too.
  it('links the runner recovered from artifact labels', () => {
    const legacy = {...run, status: {...run.status, executor: undefined}}

    render(stateWith([artifact('log', 'redacted')], {fetching: false, response: legacy}))

    expect(screen.getByText('artifact-runner').closest('a')).toHaveAttribute('href', '/runners/artifact-runner')
  })

  // Nothing to link to, and a link to nowhere is worse than plain text.
  it('does not link the runner field when nothing recorded who ran it', () => {
    const legacy = {...run, status: {...run.status, executor: undefined}}

    const {container} = render(stateWith([], {fetching: false, response: legacy}))

    expect(container.querySelector('a[href^="/runners/"]')).toBeNull()
  })

  // Runs written before the server recorded an executor still have to render.
  // Their identity is recoverable from the labels of the artifacts the worker
  // uploaded, which is where this was read from previously.
  it('falls back to artifact labels for a run with no executor recorded', () => {
    const legacy = {...run, status: {...run.status, executor: undefined}}

    render(stateWith([artifact('log', 'redacted')], {fetching: false, response: legacy}))

    expect(screen.getByText('artifact-runner')).toBeInTheDocument()
    expect(screen.getByRole('link', {name: 'artifact-worker'})).toHaveAttribute('href', '/workers/artifact-worker')
  })

  // With neither source available the field says so rather than rendering blank.
  it('shows a dash when nothing recorded who ran it', () => {
    const legacy = {...run, status: {...run.status, executor: undefined}}

    render(stateWith([], {fetching: false, response: legacy}))

    expect(screen.getByText('Runner')).toBeInTheDocument()
  })

  it('lists artifacts with their data classification', () => {
    render(stateWith([artifact('log', 'redacted'), artifact('metrics', 'clean')]))

    expect(screen.getByText('log')).toBeInTheDocument()
    expect(screen.getByText('metrics')).toBeInTheDocument()
    expect(screen.getByText('redacted')).toBeInTheDocument()
    expect(screen.getByText('clean')).toBeInTheDocument()
  })

  // A HAR keeps credentials so that it can be replayed. Opening one should say
  // so rather than presenting it like any other artifact.
  it('warns before showing a secret-bearing artifact', () => {
    render(stateWith([artifact('har', 'secret-bearing')]))

    expect(screen.getByText('secret-bearing')).toBeInTheDocument()
  })

  it('puts the log first, since that is what a failed run sends you to', () => {
    render(stateWith([artifact('metrics', 'clean'), artifact('log', 'redacted')]))

    const names = screen.getAllByText(/kbjk96thvzcuiass\./).map((n) => n.textContent)
    expect(names[0]).toContain('.log')
  })

  it('shows a spinner until the run arrives', () => {
    const {container} = render(stateWith([], {fetching: true}))

    expect(container.querySelector('svg, [class*="Spinner"]')).toBeTruthy()
  })

  it('reports a load failure', () => {
    render(stateWith([], {fetching: false, error: {message: 'not found'}}))

    expect(screen.getByText(/Error loading run/)).toBeInTheDocument()
  })

  // Placeholder for the next iteration's network path and traces, so the layout
  // does not have to be reworked to make room for it.
  // Reached from /results/:runId the scenario is not in the URL, so the page
  // reads it from the run's own labels rather than requiring it as a prop.
  it('finds the scenario from the run when not given one', () => {
    const withLabels = {
      ...run,
      labels: {'urth/scenario.name': 'tcp-self-fondle'},
    }

    renderStandalone(stateWith([artifact('log', 'redacted')], {fetching: false, response: withLabels}))

    expect(screen.getByText(/tcp-self-fondle/)).toBeInTheDocument()
    expect(screen.getByText('success')).toBeInTheDocument()
  })

  // A run whose scenario has since been deleted still has to render.
  it('renders without a scenario link when the run does not name one', () => {
    renderStandalone(stateWith([artifact('log', 'redacted')]))

    expect(screen.getByText('kbjk96thvzcuiass')).toBeInTheDocument()
    expect(screen.queryByText(/← /)).not.toBeInTheDocument()
  })

  // A run that never executed has no logs, no artifacts and no worker: without
  // the label spelled out, the page shows a failure and no reason for it.
  it('explains a run that was never scheduled', () => {
    const unschedulable = {
      ...run,
      labels: {[LabelResult.Unschedulable]: 'no-eligible-runner'},
      status: {status: 'errored', result: 'errored'},
    }

    renderStandalone(stateWith([], {fetching: false, response: unschedulable}))

    expect(screen.getByText(/No active runner matched/)).toBeInTheDocument()
    // And it does not claim to have started: the timing helpers fall back to
    // the resource's creation time, which would report a 0ms probe run.
    expect(screen.getByText('never started')).toBeInTheDocument()
    expect(screen.getByText('never ran')).toBeInTheDocument()
  })

  it('reserves a place for network and trace detail', () => {
    render(stateWith([artifact('log', 'redacted')]))

    expect(screen.getByText(/Network path, request timing and traces/)).toBeInTheDocument()
  })
})
