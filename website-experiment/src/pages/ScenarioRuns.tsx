import {useQuery} from '@tanstack/react-query'
import {ArrowLeft, Rows3} from 'lucide-react'
import {Link, useParams} from 'react-router-dom'
import {api} from '../api'
import {Pagination, RunTable, TableShell} from '../components/Data'
import {Empty, ErrorState, Loading, PageHeader} from '../components/ui'
import {useListSearch} from '../hooks'
import {PAGE_SIZE} from '../utils'

export function ScenarioRunsPage() {
  const {scenarioName = ''} = useParams()
  const name = decodeURIComponent(scenarioName)
  const {state, set} = useListSearch()
  const query = useQuery({
    queryKey: ['scenario-runs', name, state],
    queryFn: () => api.scenarios.runs(name, {page: state.page, pageSize: state.pageSize, labels: state.labels}),
    refetchInterval: 15_000,
  })
  return (
    <div className="page">
      <PageHeader eyebrow={<Link to={`/scenarios/${encodeURIComponent(name)}`}><ArrowLeft size={13} /> {name}</Link>} title="Run history" description="Complete execution history for this scenario." />
      {query.isPending && <Loading label="Loading run history" />}
      {query.isError && <ErrorState error={query.error} retry={() => query.refetch()} />}
      {query.data?.data.length === 0 && <Empty title="No runs recorded" />}
      {query.data && query.data.data.length > 0 && <TableShell footer={<Pagination page={state.page} pageSize={state.pageSize || PAGE_SIZE} total={query.data.total} onPage={(page) => set('page', page)} />}><RunTable runs={query.data.data} scenarioName={name} compareLink /></TableShell>}
    </div>
  )
}
