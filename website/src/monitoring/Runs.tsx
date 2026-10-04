import {Badge, Card, ErrorState, LoadingState, Metric} from '@sre-norns/components'
import {isAccountAdmin, usePrincipal} from '@sre-norns/components/identity'
import {Link, Navigate, useParams} from 'react-router-dom'
import {isTerminalRun, page, run, type Run} from '../api/models'
import {listPath, resourcePath, useResource} from '../api/queries'
import {accountPath, projectPath, runArtifactsPath, scenarioPath} from '../identity/links'
import {Collection, Heading, Labels} from './common'
import {LiveRunLog} from './LiveRunLog'

export function RunState({value}: {value: Run}) {
  const result = value.status?.result
  const state = result || value.status?.status || 'unknown'
  return <Badge tone={result === 'success' ? 'success' : result ? 'error' : state === 'running' || state === 'pending' ? 'attention' : 'neutral'}>{state}</Badge>
}
function duration(r: Run) {
  if (!r.spec.start_time) return 'Not started'
  const elapsed = new Date(r.spec.end_time ?? Date.now()).getTime() - new Date(r.spec.start_time).getTime()
  return Number.isFinite(elapsed) ? `${Math.max(0, elapsed / 1000).toFixed(2)} s` : 'Unknown'
}
export function Runs() {
  const {accountId = '', projectId = '', scenarioName} = useParams()
  const base = projectPath(accountId, projectId)
  const path = scenarioName ? `${resourcePath('projects', projectId, 'scenarios', scenarioName)}/results` : resourcePath('projects', projectId, 'results')
  return <>
    <Heading title={scenarioName ? `${scenarioName} runs` : 'Runs'} description="Execution history, including queued and unfinished work." />
    <Collection path={path} scope={projectId} schema={run} title="Runs" columns={[
      {header: 'Run', cell: (r) => <Link to={`${base}/runs/${encodeURIComponent(r.metadata.name)}`}>{r.metadata.name}</Link>},
      {header: 'Scenario', cell: (r) => r.metadata.labels?.['urth/scenario.name'] || 'Unknown'},
      {header: 'Result', cell: (r) => <RunState value={r} />},
      {header: 'Started', cell: (r) => r.spec.start_time ? new Date(r.spec.start_time).toLocaleString() : 'Not started'},
      {header: 'Duration', cell: duration},
      {header: 'Executor', cell: (r) => r.status?.executor?.workerName || r.status?.executor?.runnerName || 'Not assigned'},
    ]} />
  </>
}
export function RunDetail() {
  const {accountId = '', projectId = '', runName = '', scenarioName} = useParams()
  const admin = isAccountAdmin(usePrincipal())
  const path = scenarioName ? `${resourcePath('projects', projectId, 'scenarios', scenarioName)}/results/${encodeURIComponent(runName)}` : resourcePath('projects', projectId, 'results', runName)
  const query = useResource(path, run, projectId, {poll: true})
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />
  if (!query.data) return <LoadingState />
  const r = query.data.data
  const scenario = r.metadata.labels?.['urth/scenario.name']
  const scenarioLink = scenario && scenarioPath(accountId, projectId, scenario)
  const artifacts = runArtifactsPath(accountId, projectId, r.metadata.uid)
  const executor = r.status?.executor
  return <>
    <Heading title={r.metadata.name} description={scenarioLink ? <>Scenario: <Link to={scenarioLink}>{scenario}</Link></> : 'Scenario run'}><RunState value={r} /></Heading>
    <div className="grid"><Metric label="Duration" value={duration(r)} /><Metric label="Artifacts" value={<Link to={artifacts}>{r.status?.numberArtifacts ?? 0}</Link>} /><Metric label="Probe" value={r.spec.probKind || 'Unknown'} /></div>
    <Card><h2>Execution</h2>
      <p>Started: {r.spec.start_time ? new Date(r.spec.start_time).toLocaleString() : 'Not started'}</p>
      <p>Ended: {r.spec.end_time ? new Date(r.spec.end_time).toLocaleString() : 'Not finished'}</p>
      <p>Runner: {admin && executor?.runnerId ? <Link to={`${accountPath(accountId)}/runners/${encodeURIComponent(executor.runnerId)}`}>{executor.runnerName || executor.runnerId}</Link> : executor?.runnerName || 'Not assigned'}</p>
      <p>Worker: {admin && executor?.workerName ? <Link to={`${accountPath(accountId)}/workers/${encodeURIComponent(executor.workerName)}`}>{executor.workerName}</Link> : executor?.workerName || 'Not claimed'}</p>
      {r.metadata.labels?.['urth/result.unschedulable'] && <p>Placement: {r.metadata.labels['urth/result.unschedulable']}</p>}
      <Labels value={r.metadata.labels} />
      <div className="actions">{scenarioLink && <Link to={scenarioLink}>Scenario definition</Link>}<Link to={artifacts}>Run artifacts</Link></div>
    </Card>
    <Card><h2>Run log</h2>{scenario ? <LiveRunLog key={r.metadata.uid} project={projectId} scenario={scenario} run={r.metadata.name} running={['pending', 'running'].includes(r.status?.status ?? '')} terminal={isTerminalRun(r)} /> : <p>No scenario reference was recorded for this run.</p>}</Card>
  </>
}

/** Dead letters retain a run UID; product detail endpoints address names. */
export function RunReference() {
  const {accountId = '', projectId = '', runId = ''} = useParams()
  const query = useResource(listPath(resourcePath('projects', projectId, 'results'), {fields: `metadata.uid=${runId}`, limit: 1}), page(run), projectId)
  if (query.isError) return <ErrorState error={query.error} />
  if (!query.data) return <LoadingState />
  const result = query.data.data.items[0]
  if (!result) return <><Heading title="Run not found" /><p>The referenced run is no longer available in this project.</p></>
  return <Navigate replace to={`${projectPath(accountId, projectId)}/runs/${encodeURIComponent(result.metadata.name)}`} />
}
