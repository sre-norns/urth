import {useState} from 'react'
import {useMutation, useQuery, useQueryClient} from '@tanstack/react-query'
import {ArrowLeft, Pause, Play, RadioTower, ServerCog} from 'lucide-react'
import {Link, useParams} from 'react-router-dom'
import {api} from '../api'
import {labels} from '../labels'
import {Button, Card, Confirm, Empty, ErrorState, KeyValue, LabelChips, Loading, PageHeader, Stat, Status} from '../components/ui'
import {formatRelative, latestSeen, presenceLabel, requirementLines, statusTone, workerCondition} from '../utils'

export function RunnerDetailPage() {
  const {runnerName = ''} = useParams()
  const name = decodeURIComponent(runnerName)
  const client = useQueryClient()
  const [confirm, setConfirm] = useState(false)
  const runner = useQuery({queryKey: ['runner', name], queryFn: () => api.runners.get(name), refetchInterval: 15_000})
  const workers = useQuery({queryKey: ['runner-workers', name], queryFn: () => api.workers.list({labels: `${labels.runner.name}=${name}`, pageSize: 512}), refetchInterval: 15_000})
  const toggle = useMutation({
    mutationFn: async () => api.runners.update(name, {...runner.data!, spec: {...runner.data!.spec, active: !runner.data!.spec.active}}),
    onSuccess: (data) => {
      client.setQueryData(['runner', name], data)
      client.invalidateQueries({queryKey: ['runners']})
      setConfirm(false)
    },
  })
  const pause = useMutation({
    mutationFn: ({worker, paused}: {worker: string; paused: boolean}) => api.workers.pause(worker, paused),
    onSuccess: () => {
      client.invalidateQueries({queryKey: ['runner-workers', name]})
      client.invalidateQueries({queryKey: ['workers']})
    },
  })
  if (runner.isPending) return <div className="page"><Loading label="Loading runner" /></div>
  if (runner.isError) return <div className="page"><ErrorState title="Error loading runner" error={runner.error} retry={() => runner.refetch()} /></div>
  const item = runner.data
  const workerList = workers.data?.data ?? []
  const online = workerList.filter((worker) => workerCondition(worker) === 'online').length
  const paused = workerList.filter((worker) => worker.status?.paused).length
  const channel = item.status?.channel
  return (
    <div className="page">
      <PageHeader
        eyebrow={<Link to="/runners"><ArrowLeft size={13} /> All runners</Link>}
        title={<><Status value={item.spec.active ? 'active' : 'disabled'} /> {item.metadata.name}</>}
        description={item.spec.description || 'Stable scheduling channel and run queue.'}
        actions={<Button tone={item.spec.active ? 'danger' : 'primary'} onClick={() => item.spec.active ? setConfirm(true) : toggle.mutate()} disabled={toggle.isPending}>{item.spec.active ? 'Disable runner' : 'Enable runner'}</Button>}
      />
      {(toggle.isError || pause.isError) && <ErrorState title="Runner action failed" error={toggle.error || pause.error} />}
      <div className="stats-grid">
        <Stat label="Registered workers" value={item.spec.maxInstance ? `${workerList.length}/${item.spec.maxInstance}` : workerList.length} detail={item.spec.maxInstance ? 'registered / limit' : 'no instance limit'} />
        <Stat label="Online" value={`${online}/${workerList.length}`} detail="reporting on both paths" tone={online ? 'success' : undefined} />
        <Stat label="Paused" value={paused} detail="not taking jobs" tone={paused ? 'warning' : undefined} />
        {channel?.observed && <Stat label="Queue depth" value={channel.pending ?? 0} detail={`${channel.pullers ?? 0} waiting pullers`} />}
        <Stat label="Registered" value={formatRelative(item.metadata.creationTimestamp)} detail={<span className="mono">{item.metadata.uid}</span>} />
      </div>
      <div className="detail-grid">
        <Card title="Channel configuration" className="span-6">
          <KeyValue items={[
            {label: 'State', value: item.spec.active ? 'accepting placements' : 'disabled'},
            {label: 'Maximum workers', value: item.spec.maxInstance || 'unlimited', mono: true},
            {label: 'Queue observed', value: channel?.observed ? 'yes' : 'no'},
          ]} />
        </Card>
        <Card title="Registration requirements" className="span-6">
          {requirementLines(item.spec.requirements).length ? <div className="requirements">{requirementLines(item.spec.requirements).map((line) => <code key={line}>{line}</code>)}</div> : <span className="muted">No worker label requirements.</span>}
          <LabelChips labels={item.metadata.labels} />
        </Card>
      </div>
      <section className="section">
        <div className="section-heading"><div><span className="eyebrow"><ServerCog size={13} /> Consumers</span><h2>Workers on this runner</h2></div><span className="muted">{workerList.length} claiming this identity</span></div>
        {workers.isPending && <Loading label="Loading workers" />}
        {workers.isError && <ErrorState title="Workers unavailable" error={workers.error} retry={() => workers.refetch()} />}
        {!workers.isPending && !workers.isError && workerList.length === 0 && <Empty title="No workers registered" description="Runs remain queued until a process authenticates against this runner." />}
        {workerList.length > 0 && <div className="table-shell"><div className="table-scroll"><table>
          <thead><tr><th>Worker</th><th>Presence</th><th>Host</th><th>Last contact</th><th>Jobs</th><th className="align-right">Action</th></tr></thead>
          <tbody>{workerList.map((worker) => {
            const condition = workerCondition(worker)
            return <tr key={worker.metadata.uid || worker.metadata.name}>
              <td><span className={`presence-dot tone-${statusTone(condition)}`} /><Link className="mono" to={`/workers/${encodeURIComponent(worker.metadata.name)}`}>{worker.metadata.name}</Link><span className="table-secondary">{worker.status?.paused ? 'paused by operator' : 'available'}</span></td>
              <td><Status value={worker.status?.paused ? 'paused' : presenceLabel(condition)} title={presenceLabel(condition)} /></td>
              <td>{worker.metadata.labels?.[labels.worker.hostname] || 'unknown'}<span className="table-secondary mono">{[worker.metadata.labels?.[labels.worker.os], worker.metadata.labels?.[labels.worker.arch]].filter(Boolean).join('/')}</span></td>
              <td>{formatRelative(latestSeen(worker))}<span className="table-secondary">{worker.status?.lastSeenVia || 'not observed'}</span></td>
              <td className="muted">Open worker</td>
              <td className="align-right"><Button tone="quiet" disabled={pause.isPending} onClick={() => pause.mutate({worker: worker.metadata.name, paused: !worker.status?.paused})}>{worker.status?.paused ? <Play size={14} /> : <Pause size={14} />}{worker.status?.paused ? 'Resume' : 'Pause'}</Button></td>
            </tr>
          })}</tbody>
        </table></div></div>}
      </section>
      <Confirm open={confirm} title={`Disable ${name}?`} description={`Its ${workerList.length} worker(s) will stop taking new jobs and new workers cannot register. Runs already in progress are unaffected.`} confirmLabel="Disable runner" busy={toggle.isPending} onCancel={() => setConfirm(false)} onConfirm={() => toggle.mutate()} />
    </div>
  )
}
