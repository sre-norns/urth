import {useState} from 'react'
import {useMutation, useQuery, useQueryClient} from '@tanstack/react-query'
import {ArrowLeft, Edit3, Play, Radio, Rows3} from 'lucide-react'
import {Link, useNavigate, useParams} from 'react-router-dom'
import {api} from '../api'
import {Heartbeat, RunTable} from '../components/Data'
import {Button, Card, Empty, ErrorState, KeyValue, LabelChips, LinkButton, Loading, PageHeader, Stat, Status} from '../components/ui'
import {formatDuration, formatPercent, formatRelative, requirementLines, runSummary} from '../utils'

function placementMessage(placement?: Awaited<ReturnType<typeof api.scenarios.placement>>) {
  if (!placement) return null
  if (placement.schedulable && !placement.readyWorkers) return 'A matching runner can accept this run; it will remain queued until a worker connects.'
  if (placement.schedulable) return `${placement.readyWorkers ?? 0} worker(s) are ready to collect this run.`
  if (!placement.matchingRunners) return `No runner matches ${placement.requirements || 'this scenario’s requirements'}.`
  return `${placement.matchingRunners} runner(s) match, but none is currently eligible.`
}

export function ScenarioDetailPage() {
  const {scenarioName = ''} = useParams()
  const name = decodeURIComponent(scenarioName)
  const navigate = useNavigate()
  const client = useQueryClient()
  const [actionError, setActionError] = useState<unknown>()
  const scenario = useQuery({queryKey: ['scenario', name], queryFn: () => api.scenarios.get(name)})
  const runs = useQuery({
    queryKey: ['scenario-runs', name, 20],
    queryFn: () => api.scenarios.runs(name, {pageSize: 20}),
    refetchInterval: 15_000,
  })
  const placement = useQuery({
    queryKey: ['scenario-placement', name],
    queryFn: () => api.scenarios.placement(name),
    retry: false,
  })
  const runNow = useMutation({
    mutationFn: () => api.scenarios.runNow(name),
    onSuccess: (run) => {
      client.invalidateQueries({queryKey: ['scenario-runs', name]})
      navigate(`/scenarios/${encodeURIComponent(name)}/runs/${encodeURIComponent(run.name)}`)
    },
    onError: setActionError,
  })

  if (scenario.isPending) return <div className="page"><Loading label="Loading scenario" /></div>
  if (scenario.isError) return <div className="page"><ErrorState title="Error loading scenario" error={scenario.error} retry={() => scenario.refetch()} /></div>

  const item = scenario.data
  const history = runs.data?.data ?? []
  const summary = runSummary(history)
  const latest = history[0]
  const canRun = Boolean(item.spec.active && item.spec.prob?.kind && placement.data?.schedulable !== false)

  return (
    <div className="page">
      <PageHeader
        eyebrow={<Link to="/scenarios"><ArrowLeft size={13} /> All scenarios</Link>}
        title={<><Status value={!item.spec.active ? 'paused' : latest ? latest.status?.result || latest.status?.status : 'new'} /> {item.metadata.name}</>}
        description={item.spec.description || 'No description provided.'}
        actions={
          <>
            <LinkButton to={`/scenarios/${encodeURIComponent(name)}/edit`} tone="quiet"><Edit3 size={16} /> Edit</LinkButton>
            <Button disabled={!canRun || runNow.isPending} onClick={() => runNow.mutate()}><Play size={16} /> {runNow.isPending ? 'Queueing…' : 'Run now'}</Button>
          </>
        }
      />
      {Boolean(actionError) && <ErrorState title="Run could not be queued" error={actionError} />}
      {placementMessage(placement.data) && <div className={`notice ${canRun ? 'notice-info' : 'notice-warning'}`}><Radio size={17} /><span>{placementMessage(placement.data)}</span></div>}
      <div className="stats-grid">
        <Stat label="Success rate" value={formatPercent(summary.successRate)} detail={`${summary.successes} of ${summary.settled} settled`} tone={summary.successRate === 1 ? 'success' : undefined} />
        <Stat label="Average duration" value={formatDuration(summary.average)} detail={`last ${history.length} runs`} />
        <Stat label="Last run" value={latest ? formatRelative(latest.spec?.start_time || latest.creationTimestamp) : 'never'} detail={latest?.status?.result || latest?.status?.status} />
        <Stat label="Next scheduled" value={item.status?.nextScheduledRunTime ? formatRelative(item.status.nextScheduledRunTime) : 'not scheduled'} detail={item.spec.schedule || 'manual only'} />
        <Stat label="Capacity" value={`${placement.data?.eligibleRunners ?? '—'} / ${placement.data?.readyWorkers ?? '—'}`} detail="eligible runners / ready workers" />
      </div>
      <div className="detail-grid">
        <Card title="Service heartbeat" meta="oldest → newest" className="span-8">
          {history.length ? <Heartbeat runs={history} limit={60} /> : <Empty title="No health signal yet" />}
        </Card>
        <Card title="Execution" className="span-4">
          <KeyValue items={[
            {label: 'Probe', value: item.spec.prob?.kind || 'not configured', mono: true},
            {label: 'Schedule', value: item.spec.schedule || 'manual only', mono: true},
            {label: 'State', value: item.spec.active ? <Status value="active" /> : <Status value="paused" />},
            {label: 'Created', value: formatRelative(item.metadata.creationTimestamp)},
          ]} />
        </Card>
        <Card title="Channel requirements" className="span-6">
          {requirementLines(item.spec.requirements).length ? (
            <div className="requirements">{requirementLines(item.spec.requirements).map((line) => <code key={line}>{line}</code>)}</div>
          ) : <span className="muted">Any active runner may accept this scenario.</span>}
        </Card>
        <Card title="Labels" className="span-6"><LabelChips labels={item.metadata.labels} /></Card>
      </div>
      <section className="section">
        <div className="section-heading"><div><span className="eyebrow"><Rows3 size={13} /> Recent executions</span><h2>Recent runs</h2></div><Link to={`/scenarios/${encodeURIComponent(name)}/runs`}>View all runs →</Link></div>
        {runs.isPending && <Loading label="Loading runs" />}
        {runs.isError && <ErrorState title="Run history unavailable" error={runs.error} retry={() => runs.refetch()} />}
        {!runs.isPending && !runs.isError && history.length === 0 && <Empty title="This scenario has not run" description="Trigger it manually or wait for its schedule." />}
        {history.length > 0 && <div className="table-shell"><RunTable runs={history.slice(0, 10)} scenarioName={name} compareLink /></div>}
      </section>
    </div>
  )
}
