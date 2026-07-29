import {useQuery} from '@tanstack/react-query'
import {RadioTower} from 'lucide-react'
import {Link} from 'react-router-dom'
import {api} from '../api'
import {Pagination, TableShell} from '../components/Data'
import {SearchToolbar} from '../components/SearchToolbar'
import {Empty, ErrorState, LabelChips, Loading, PageHeader, Status} from '../components/ui'
import {useListSearch} from '../hooks'
import {formatRelative, PAGE_SIZE, requirementLines} from '../utils'

export function RunnersPage() {
  const {state, set} = useListSearch()
  const query = useQuery({
    queryKey: ['runners', state],
    queryFn: () => api.runners.list({name: state.name, labels: state.labels, page: state.page, pageSize: state.pageSize}),
    refetchInterval: 15_000,
  })
  return (
    <div className="page">
      <PageHeader eyebrow={<><RadioTower size={13} /> Infrastructure</>} title="Runners" description="Stable run queues used for placement and dispatch." />
      <SearchToolbar name={state.name} labels={state.labels} onName={(value) => set('name', value)} onLabels={(value) => set('labels', value)} />
      {query.isPending && <Loading label="Loading runners" />}
      {query.isError && <ErrorState error={query.error} retry={() => query.refetch()} />}
      {query.data?.data.length === 0 && <Empty title="No runners found" />}
      {query.data && query.data.data.length > 0 && (
        <TableShell footer={<Pagination page={state.page} pageSize={state.pageSize || PAGE_SIZE} total={query.data.total} onPage={(page) => set('page', page)} />}>
          <div className="table-scroll"><table>
            <thead><tr><th>Runner</th><th>State</th><th>Workers</th><th>Queue</th><th>Requirements</th><th>Labels</th><th>Registered</th></tr></thead>
            <tbody>{query.data.data.map((runner) => {
              const channel = runner.status?.channel
              return <tr key={runner.metadata.uid || runner.metadata.name}>
                <td><Link className="table-primary" to={`/runners/${encodeURIComponent(runner.metadata.name)}`}>{runner.metadata.name}</Link><span className="table-secondary">{runner.spec.description || 'Scheduling channel'}</span></td>
                <td><Status value={runner.spec.active ? 'active' : 'disabled'} /></td>
                <td><strong className="mono">{runner.status?.numberInstances ?? runner.status?.activeInstances?.length ?? 0}</strong>{runner.spec.maxInstance ? <span className="table-secondary">limit {runner.spec.maxInstance}</span> : null}</td>
                <td>{channel?.observed ? <><span className="table-primary mono">{channel.pending ?? 0} queued</span><span className="table-secondary">{channel.pullers ?? 0} pullers</span></> : <span className="muted">Not observed</span>}</td>
                <td><div className="requirements compact">{requirementLines(runner.spec.requirements).map((line) => <code key={line}>{line}</code>)}</div></td>
                <td><LabelChips labels={runner.metadata.labels} onClick={(key, value) => set('labels', `${key} = ${value}`)} /></td>
                <td>{formatRelative(runner.metadata.creationTimestamp)}</td>
              </tr>
            })}</tbody>
          </table></div>
        </TableShell>
      )}
    </div>
  )
}
