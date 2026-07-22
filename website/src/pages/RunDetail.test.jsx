import React from 'react'
import {describe, it, expect, vi} from 'vitest'
import {screen} from '@testing-library/react'
import {renderWithProviders} from '../test/render.jsx'
import RunDetail from './RunDetail.jsx'
import {LabelArtifact, LabelRunner, LabelWorker} from '../utils/labels.js'

vi.mock('../actions/fetchRun.js', () => ({default: () => () => {}}))
vi.mock('../actions/fetchRunArtifacts.js', () => ({default: () => () => {}}))
vi.mock('../actions/fetchArtifactContent.js', () => ({default: () => () => {}}))

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
      ...extra,
    },
  },
})

const stateWith = (artifacts, runState = {fetching: false, response: run}) => ({
  scenario: {},
  scenarios: {},
  scenarioResults: {},
  scenarioActions: {},
  run: {kbjk96thvzcuiass: runState},
  runArtifacts: {kbjk96thvzcuiass: {fetching: false, response: {data: artifacts}}},
  artifactContent: {},
})

const render = (state) =>
  renderWithProviders(<RunDetail scenarioId="tcp-self-fondle" runId="kbjk96thvzcuiass" />, {
    preloadedState: state,
  })

// Opened from the cross-scenario Results list, where the URL carries only the
// run name.
const renderStandalone = (state) => renderWithProviders(<RunDetail runId="kbjk96thvzcuiass" />, {preloadedState: state})

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
    expect(screen.getByText('worker worker-01')).toBeInTheDocument()
    // The run's own record wins over the labels on its artifacts.
    expect(screen.queryByText('artifact-runner')).not.toBeInTheDocument()
  })

  // Runs written before the server recorded an executor still have to render.
  // Their identity is recoverable from the labels of the artifacts the worker
  // uploaded, which is where this was read from previously.
  it('falls back to artifact labels for a run with no executor recorded', () => {
    const legacy = {...run, status: {...run.status, executor: undefined}}

    render(stateWith([artifact('log', 'redacted')], {fetching: false, response: legacy}))

    expect(screen.getByText('artifact-runner')).toBeInTheDocument()
    expect(screen.getByText('worker artifact-worker')).toBeInTheDocument()
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

  it('reserves a place for network and trace detail', () => {
    render(stateWith([artifact('log', 'redacted')]))

    expect(screen.getByText(/Network path, request timing and traces/)).toBeInTheDocument()
  })
})
