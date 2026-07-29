import {useQuery} from '@tanstack/react-query'
import {ServerCog} from 'lucide-react'
import {Link} from 'react-router-dom'
import {api} from '../api'
import {Pagination, TableShell} from '../components/Data'
import {SearchToolbar} from '../components/SearchToolbar'
import {Empty, ErrorState, LabelChips, Loading, PageHeader, Presence, Status} from '../components/ui'
import {useListSearch} from '../hooks'
import {labels, workerRunnerName} from '../labels'
import {formatRelative, latestSeen, PAGE_SIZE, workerCondition} from '../utils'

export function WorkersPage() {
  const {state, set} = useListSearch()
  const query = useQuery({
    queryKey: ['workers', state],
    queryFn: () => api.workers.list({name: state.name, labels: state.labels, page: state.page, pageSize: state.pageSize}),
    refetchInterval: 15_000,
  })
  return (
    <div className="page">
      <PageHeader eyebrow={<><ServerCog size={13} /> Infrastructure</>} title="Workers" description="Processes currently or historically registered against runner queues." />
      <SearchToolbar name={state.name} labels={state.labels} onName={(value) => set('name', value)} onLabels={(value) => set('labels', value)} />
      {query.isPending && <Loading label="Loading workers" />}
      {query.isError && <ErrorState error={query.error} retry={() => query.refetch()} />}
      {query.data?.data.length === 0 && <Empty title="No workers found" />}
      {query.data && query.data.data.length > 0 && (
        <TableShell footer={<Pagination page={state.page} pageSize={state.pageSize || PAGE_SIZE} total={query.data.total} onPage={(page) => set('page', page)} />}>
          <div className="table-scroll"><table>
            <thead><tr><th>Worker</th><th>Presence</th><th>Runner</th><th>Host</th><th>Platform</th><th>Last contact</th><th>State</th><th>Labels</th></tr></thead>
            <tbody>{query.data.data.map((worker) => {
              const runner = workerRunnerName(worker)
              const workerLabels = worker.metadata.labels ?? {}
              return <tr key={worker.metadata.uid || worker.metadata.name}>
                <td><Link className="mono table-primary" to={`/workers/${encodeURIComponent(worker.metadata.name)}`}>{worker.metadata.name}</Link><span className="table-secondary">{workerLabels[labels.worker.build] || 'version unknown'}</span></td>
                <td><Presence condition={workerCondition(worker)} /></td>
                <td>{runner ? <Link to={`/runners/${encodeURIComponent(runner)}`}>{runner}</Link> : <span className="muted">Unknown runner</span>}</td>
                <td>{workerLabels[labels.worker.hostname] || 'unknown'}</td>
                <td className="mono">{[workerLabels[labels.worker.os], workerLabels[labels.worker.arch]].filter(Boolean).join('/') || 'unknown'}</td>
                <td><span className="table-primary">{formatRelative(latestSeen(worker))}</span><span className="table-secondary">{worker.status?.lastSeenVia === 'claim' ? 'claimed a run' : 'heartbeat'}</span></td>
                <td>{worker.status?.paused ? <Status value="paused" /> : <span className="muted">accepting jobs</span>}</td>
                <td><LabelChips labels={workerLabels} onClick={(key, value) => set('labels', `${key} = ${value}`)} /></td>
              </tr>
            })}</tbody>
          </table></div>
        </TableShell>
      )}
    </div>
  )
}
