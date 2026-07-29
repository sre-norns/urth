import {useQuery} from '@tanstack/react-query'
import {Box, ShieldAlert} from 'lucide-react'
import {Link} from 'react-router-dom'
import {api} from '../api'
import {artifactDataClass, artifactKind, artifactMime} from '../labels'
import {artifactResultName, Pagination, TableShell} from '../components/Data'
import {SearchToolbar} from '../components/SearchToolbar'
import {Empty, ErrorState, Loading, PageHeader, Status} from '../components/ui'
import {useListSearch} from '../hooks'
import {formatRelative, PAGE_SIZE} from '../utils'

export function ArtifactsPage() {
  const {state, set} = useListSearch()
  const query = useQuery({
    queryKey: ['artifacts', state],
    queryFn: () => api.artifacts.list({name: state.name, labels: state.labels, page: state.page, pageSize: state.pageSize}),
  })
  return (
    <div className="page">
      <PageHeader eyebrow={<><Box size={13} /> Run data</>} title="Artifacts" description="Logs, metrics, traces, and captures produced by scenario runs." />
      <SearchToolbar name={state.name} labels={state.labels} onName={(value) => set('name', value)} onLabels={(value) => set('labels', value)} />
      {query.isPending && <Loading label="Loading artifacts" />}
      {query.isError && <ErrorState error={query.error} retry={() => query.refetch()} />}
      {query.data?.data.length === 0 && <Empty title="No artifacts found" />}
      {query.data && query.data.data.length > 0 && (
        <TableShell footer={<Pagination page={state.page} pageSize={state.pageSize || PAGE_SIZE} total={query.data.total} onPage={(page) => set('page', page)} />}>
          <div className="table-scroll"><table>
            <thead><tr><th>Artifact</th><th>Kind</th><th>Data class</th><th>MIME type</th><th>Run</th><th>Created</th><th>Retention</th></tr></thead>
            <tbody>{query.data.data.map((artifact) => {
              const run = artifactResultName(artifact)
              const classification = artifactDataClass(artifact)
              return <tr key={artifact.metadata.uid || artifact.metadata.name}>
                <td><Link className="mono table-primary" to={`/artifacts/${encodeURIComponent(artifact.metadata.name)}`}>{artifact.metadata.name}</Link></td>
                <td><span className="artifact-kind"><Box size={14} />{artifactKind(artifact)}</span></td>
                <td><Status value={classification} />{['unknown', 'secret-bearing'].includes(classification) && <ShieldAlert className="inline-warning" size={15} aria-label="May contain secrets" />}</td>
                <td className="mono">{artifactMime(artifact)}</td>
                <td>{run ? <Link className="mono" to={`/runs/${encodeURIComponent(run)}`}>{run}</Link> : <span className="muted">unknown</span>}</td>
                <td>{formatRelative(artifact.metadata.creationTimestamp)}</td>
                <td>{artifact.spec.expireTime || artifact.spec.expire_time ? `expires ${formatRelative(artifact.spec.expireTime || artifact.spec.expire_time)}` : 'pinned'}</td>
              </tr>
            })}</tbody>
          </table></div>
        </TableShell>
      )}
    </div>
  )
}
