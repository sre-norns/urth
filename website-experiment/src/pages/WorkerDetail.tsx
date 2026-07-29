import {useQuery, useMutation, useQueryClient} from '@tanstack/react-query'
import {ArrowLeft, Pause, Play, ServerCog} from 'lucide-react'
import {Link, useParams} from 'react-router-dom'
import {api} from '../api'
import {labels, workerRunnerName} from '../labels'
import {RunTable} from '../components/Data'
import {Button, Card, Empty, ErrorState, KeyValue, LabelChips, Loading, PageHeader, Stat, Status} from '../components/ui'
import {formatRelative, formatTimestamp, latestSeen, presenceLabel, statusTone, workerCondition} from '../utils'

export function WorkerDetailPage() {
  const {workerName = ''} = useParams()
  const name = decodeURIComponent(workerName)
  const client = useQueryClient()
  const worker = useQuery({queryKey: ['worker', name], queryFn: () => api.workers.get(name), refetchInterval: 15_000})
  const runs = useQuery({queryKey: ['worker-runs', name], queryFn: () => api.runs.list({labels: `${labels.worker.name}=${name}`, pageSize: 20}), refetchInterval: 15_000})
  const pause = useMutation({
    mutationFn: (paused: boolean) => api.workers.pause(name, paused),
    onSuccess: (data) => {
      client.setQueryData(['worker', name], data)
      client.invalidateQueries({queryKey: ['workers']})
      client.invalidateQueries({queryKey: ['runner-workers']})
    },
  })
  if (worker.isPending) return <div className="page"><Loading label="Loading worker" /></div>
  if (worker.isError) return <div className="page"><ErrorState title="Error loading worker" error={worker.error} retry={() => worker.refetch()} /></div>
  const item = worker.data
  const runner = workerRunnerName(item)
  const condition = workerCondition(item)
  const itemLabels = item.metadata.labels ?? {}
  const runList = runs.data?.data ?? []
  return (
    <div className="page">
      <PageHeader
        eyebrow={runner ? <Link to={`/runners/${encodeURIComponent(runner)}`}><ArrowLeft size={13} /> Runner {runner}</Link> : <Link to="/workers"><ArrowLeft size={13} /> All workers</Link>}
        title={<><span className={`presence-dot large tone-${statusTone(condition)}`} /><span className="mono">{item.metadata.name}</span></>}
        description={`Process registered against ${runner || 'an unknown runner'} · ${presenceLabel(condition)}`}
        actions={<Button tone={item.status?.paused ? 'primary' : 'danger'} disabled={pause.isPending} onClick={() => pause.mutate(!item.status?.paused)}>{item.status?.paused ? <Play size={16} /> : <Pause size={16} />}{item.status?.paused ? 'Resume worker' : 'Pause worker'}</Button>}
      />
      {pause.isError && <ErrorState title="Worker action failed" error={pause.error} />}
      <div className="stats-grid">
        <Stat label="Presence" value={<Status value={presenceLabel(condition)} />} detail={condition === 'online' ? 'both paths reporting' : condition.replaceAll('-', ' ')} tone={statusTone(condition)} />
        <Stat label="Jobs run" value={runs.data?.total ?? '—'} detail={`showing ${runList.length} recent`} />
        <Stat label="Registered" value={formatRelative(item.metadata.creationTimestamp)} detail={formatTimestamp(item.metadata.creationTimestamp)} />
        <Stat label="Last contact" value={formatRelative(latestSeen(item))} detail={item.status?.lastSeenVia === 'claim' ? 'claimed a run' : item.status?.lastSeenVia || 'not observed'} />
        <Stat label="Operator state" value={item.status?.paused ? 'paused' : 'available'} detail={item.status?.paused ? 'not taking jobs' : 'eligible for work'} tone={item.status?.paused ? 'warning' : undefined} />
      </div>
      <div className="detail-grid">
        <Card title="Process identity" className="span-5">
          <KeyValue items={[
            {label: 'Worker UID', value: item.metadata.uid, mono: true},
            {label: 'Runner', value: runner ? <Link to={`/runners/${encodeURIComponent(runner)}`}>{runner}</Link> : 'unknown'},
            {label: 'Host', value: itemLabels[labels.worker.hostname] || 'unknown', mono: true},
            {label: 'Platform', value: [itemLabels[labels.worker.os], itemLabels[labels.worker.arch]].filter(Boolean).join('/') || 'unknown', mono: true},
            {label: 'Build', value: itemLabels[labels.worker.build] || 'unknown', mono: true},
            {label: 'Requested TTL', value: item.spec.requestedTTL || 'server default', mono: true},
          ]} />
        </Card>
        <Card title="Presence signals" className="span-7">
          <div className="signal-grid">
            <div><span>API server</span><Status value={item.status?.presence?.api || 'unknown'} /><strong>{formatRelative(item.status?.lastSeenTime)}</strong><small>{formatTimestamp(item.status?.lastSeenTime)} · evidence: {item.status?.lastSeenVia || 'none'}</small></div>
            <div><span>Runner queue</span><Status value={item.status?.presence?.nats || 'unknown'} /><strong>{formatRelative(item.status?.natsLastSeenTime)}</strong><small>{formatTimestamp(item.status?.natsLastSeenTime)} · NATS observation</small></div>
          </div>
          {item.status?.leftAt && <div className="notice notice-warning">Worker announced departure {formatRelative(item.status.leftAt)} ({formatTimestamp(item.status.leftAt)}).</div>}
        </Card>
        <Card title="Declared labels" className="span-12"><LabelChips labels={item.metadata.labels} includeSystem /></Card>
      </div>
      <section className="section transparent-section" aria-label="Recent runs">
        <div className="section-heading"><div><span className="eyebrow"><ServerCog size={13} /> Work history</span><h2>Recent runs</h2></div><span className="muted">Showing {runList.length} of {runs.data?.total ?? 0} jobs</span></div>
        {runs.isPending && <Loading label="Loading recent runs" />}
        {runs.isError && <ErrorState title="Error loading this worker’s runs" error={runs.error} retry={() => runs.refetch()} />}
        {!runs.isPending && !runs.isError && runList.length === 0 && <Empty title="No runs recorded for this worker" />}
        {runList.length > 0 && <div className="table-shell"><RunTable runs={runList} /></div>}
      </section>
    </div>
  )
}
