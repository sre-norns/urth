import {useMemo} from 'react'
import {useQuery} from '@tanstack/react-query'
import {ArrowDown, ArrowLeft, Box, Clock3, GitCompareArrows, RadioTower, ScrollText, ServerCog} from 'lucide-react'
import {Link, useParams, useSearchParams} from 'react-router-dom'
import {api} from '../api'
import {artifactDataClass, artifactKind, labels, runScenarioName} from '../labels'
import type {Run} from '../types'
import {displayOutcome, formatDuration, formatRelative, formatTimestamp, presenceLabel, runDuration, statusTone, workerCondition} from '../utils'
import {Card, Empty, ErrorState, KeyValue, Loading, PageHeader, Stat, Status} from '../components/ui'
import {LiveRunLog} from '../components/LiveRunLog'

function delta(current: number | null, baseline: number | null) {
  if (current === null || baseline === null) return 'Not comparable'
  const difference = current - baseline
  if (difference === 0) return 'No change'
  return `${difference > 0 ? '+' : '−'}${formatDuration(Math.abs(difference))}`
}

function RunTimeline({run}: {run: Run}) {
  const created = run.creationTimestamp
  const started = run.spec?.start_time
  const ended = run.spec?.end_time
  return (
    <ol className="timeline">
      <li className="complete"><i /><div><strong>Run created</strong><span>{formatTimestamp(created)}</span></div></li>
      <li className={started ? 'complete' : ''}><i /><div><strong>{started ? 'Worker claimed run' : 'Waiting for worker'}</strong><span>{started ? formatTimestamp(started) : 'No start time recorded'}</span></div></li>
      <li className={ended ? 'complete' : run.status?.status === 'errored' ? 'failed' : ''}><i /><div><strong>{ended ? `Probe ${run.status?.result || 'finished'}` : run.status?.status === 'errored' ? 'Dispatch failed' : 'Execution in progress'}</strong><span>{ended ? formatTimestamp(ended) : run.status?.deadline ? `deadline ${formatTimestamp(run.status.deadline)}` : 'No completion time recorded'}</span></div></li>
    </ol>
  )
}

export function RunDetailPage() {
  const {scenarioName: scenarioParam, runName = ''} = useParams()
  const runId = decodeURIComponent(runName)
  const [search, setSearch] = useSearchParams()
  const runQuery = useQuery({
    queryKey: ['run', runId],
    queryFn: () => api.runs.get(runId),
    refetchInterval: (query) => ['running', 'pending'].includes(query.state.data?.status?.status || '') ? 3_000 : false,
  })
  const run = runQuery.data
  const scenario = scenarioParam ? decodeURIComponent(scenarioParam) : runScenarioName(run)
  const artifacts = useQuery({
    queryKey: ['run-artifacts', runId],
    queryFn: () => api.artifacts.list({labels: `${labels.artifact.resultName}=${runId}`, pageSize: 100}),
  })
  const history = useQuery({
    queryKey: ['scenario-runs', scenario, 'comparison'],
    queryFn: () => api.scenarios.runs(scenario!, {pageSize: 100}),
    enabled: Boolean(scenario),
  })
  const workerName = run?.status?.executor?.workerName
  const worker = useQuery({
    queryKey: ['worker', workerName],
    queryFn: () => api.workers.get(workerName!),
    enabled: Boolean(workerName),
    retry: false,
    refetchInterval: 15_000,
  })
  const baseline = useMemo(() => {
    const candidates = history.data?.data ?? []
    const requested = search.get('baseline')
    if (requested) return candidates.find((item) => item.name === requested)
    const index = candidates.findIndex((item) => item.name === runId)
    return index >= 0 ? candidates[index + 1] : candidates.find((item) => item.name !== runId)
  }, [history.data, runId, search])

  if (runQuery.isPending) return <div className="page"><Loading label="Loading run" /></div>
  if (runQuery.isError) return <div className="page"><ErrorState title="Error loading run" error={runQuery.error} retry={() => runQuery.refetch()} /></div>
  if (!run) return <div className="page"><Loading label="Loading run" /></div>

  const executor = run.status?.executor
  const workerMatches = Boolean(executor?.workerId && worker.data?.metadata.uid === executor.workerId)
  const currentCondition = workerMatches ? workerCondition(worker.data) : 'unknown'
  const neverRan = Boolean(run.labels?.[labels.result.unschedulable])
  const baselineOptions = (history.data?.data ?? []).filter((item) => item.name !== run.name)

  return (
    <div className="page">
      <PageHeader
        eyebrow={scenario ? <Link to={`/scenarios/${encodeURIComponent(scenario)}/runs`}><ArrowLeft size={13} /> {scenario} runs</Link> : <Link to="/runs"><ArrowLeft size={13} /> All runs</Link>}
        title={<><Status run={run} /> <span className="mono">{run.name}</span></>}
        description={`${run.spec?.probKind || 'Unknown'} probe · ${run.status?.status || 'unknown'} lifecycle`}
      />
      {neverRan && <div className="notice notice-warning"><Clock3 size={17} /><span>This run never started: {run.labels?.[labels.result.unschedulable]?.replaceAll('-', ' ')}.</span></div>}
      <div className="stats-grid">
        <Stat label="Outcome" value={displayOutcome(run)} detail={`job ${run.status?.status || 'unknown'}`} tone={statusTone(run)} />
        <Stat label="Duration" value={neverRan ? 'never ran' : formatDuration(runDuration(run))} detail={run.spec?.start_time ? `started ${formatRelative(run.spec.start_time)}` : 'never started'} />
        <Stat label="Runner" value={executor?.runnerName ? <Link to={`/runners/${encodeURIComponent(executor.runnerName)}`}>{executor.runnerName}</Link> : '—'} detail={executor?.runnerId ? <span className="mono">{executor.runnerId}</span> : 'not placed'} />
        <Stat
          label="Worker"
          value={executor?.workerName ? <><span className={`presence-dot tone-${statusTone(currentCondition)}`} title={workerMatches ? `Current presence: ${presenceLabel(currentCondition)}` : 'Current presence unknown: historical registration'} /><Link to={`/workers/${encodeURIComponent(executor.workerName)}`}>{executor.workerName}</Link></> : '—'}
          detail={executor?.workerName ? workerMatches ? presenceLabel(currentCondition) : 'historical presence unknown' : 'never claimed'}
        />
        <Stat label="Artifacts" value={artifacts.data?.total ?? run.status?.numberArtifacts ?? 0} detail="captured outputs" />
      </div>
      <div className="detail-grid">
        <Card title="Execution timeline" className="span-5"><RunTimeline run={run} /></Card>
        <Card title="Run metadata" className="span-7">
          <KeyValue items={[
            {label: 'Run UID', value: run.uid, mono: true},
            {label: 'Created', value: formatTimestamp(run.creationTimestamp), mono: true},
            {label: 'Started', value: formatTimestamp(run.spec?.start_time), mono: true},
            {label: 'Finished', value: formatTimestamp(run.spec?.end_time), mono: true},
            {label: 'Probe kind', value: run.spec?.probKind, mono: true},
            {label: 'Lifecycle', value: run.status?.status, mono: true},
          ]} />
        </Card>
      </div>
      {scenario && (
        <Card title={<><GitCompareArrows size={17} /> Compare with another run</>} meta={
          <label className="compare-select"><span className="sr-only">Comparison baseline</span><select value={baseline?.name || ''} onChange={(event) => setSearch(event.target.value ? {baseline: event.target.value} : {})}><option value="">No earlier run</option>{baselineOptions.map((item) => <option value={item.name} key={item.name}>{item.name} · {displayOutcome(item)} · {formatRelative(item.spec?.start_time || item.creationTimestamp)}</option>)}</select></label>
        }>
          {!baseline ? <Empty title="No earlier run to compare" description="Comparison becomes available after this scenario has run more than once." /> : (
            <div className="comparison">
              <div className="comparison-head"><span>Attribute</span><strong>Current run</strong><strong>Baseline</strong><span>Change</span></div>
              {[
                ['Outcome', displayOutcome(run), displayOutcome(baseline), displayOutcome(run) === displayOutcome(baseline) ? 'Unchanged' : `${displayOutcome(baseline)} → ${displayOutcome(run)}`],
                ['Duration', formatDuration(runDuration(run)), formatDuration(runDuration(baseline)), delta(runDuration(run), runDuration(baseline))],
                ['Runner', executor?.runnerName || '—', baseline.status?.executor?.runnerName || '—', executor?.runnerName === baseline.status?.executor?.runnerName ? 'Same placement' : 'Changed'],
                ['Worker', executor?.workerName || '—', baseline.status?.executor?.workerName || '—', executor?.workerName === baseline.status?.executor?.workerName ? 'Same worker' : 'Changed'],
                ['Artifacts', String(run.status?.numberArtifacts ?? 0), String(baseline.status?.numberArtifacts ?? 0), String((run.status?.numberArtifacts ?? 0) - (baseline.status?.numberArtifacts ?? 0))],
              ].map((row) => <div className="comparison-row" key={row[0]}><span>{row[0]}</span><strong className="mono">{row[1]}</strong><span className="mono">{row[2]}</span><span>{row[3]}</span></div>)}
            </div>
          )}
        </Card>
      )}
      <div className="detail-grid">
        <Card title={<><ScrollText size={17} /> Run log</>} meta={run.status?.status === 'running' ? 'streaming output' : 'stored output'} className="span-8 log-card">
          {scenario ? <LiveRunLog scenarioName={scenario} runName={runId} running={run.status?.status === 'running'} /> : <Empty title="Log unavailable" description="This historical run does not identify its scenario." />}
        </Card>
        <Card title={<><Box size={17} /> Artifacts</>} className="span-4 artifact-list">
          {artifacts.isPending && <Loading label="Loading artifacts" />}
          {artifacts.isError && <ErrorState title="Artifacts unavailable" error={artifacts.error} retry={() => artifacts.refetch()} />}
          {artifacts.data?.data.length === 0 && <Empty title="No artifacts" />}
          {artifacts.data?.data.sort((a, b) => Number(artifactKind(b) === 'log') - Number(artifactKind(a) === 'log')).map((artifact) => (
            <Link to={`/artifacts/${encodeURIComponent(artifact.metadata.name)}`} key={artifact.metadata.uid || artifact.metadata.name}>
              <span className="artifact-icon"><Box size={15} /></span>
              <span><strong>{artifactKind(artifact)}</strong><small>{artifact.metadata.name}</small></span>
              <Status value={artifactDataClass(artifact)} subtle />
            </Link>
          ))}
        </Card>
      </div>
    </div>
  )
}
