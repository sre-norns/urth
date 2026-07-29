import {useQuery} from '@tanstack/react-query'
import {PlaySquare} from 'lucide-react'
import {RunTable, Pagination, TableShell} from '../components/Data'
import {SearchToolbar} from '../components/SearchToolbar'
import {Empty, ErrorState, Loading, PageHeader} from '../components/ui'
import {useListSearch} from '../hooks'
import {PAGE_SIZE} from '../utils'

export function RunsPage() {
  const {state, set} = useListSearch()
  const query = useQuery({
    queryKey: ['runs', state],
    queryFn: () => api.runs.list({name: state.name, labels: state.labels, page: state.page, pageSize: state.pageSize}),
    refetchInterval: 15_000,
  })
  return (
    <div className="page">
      <PageHeader eyebrow={<><PlaySquare size={13} /> Monitor</>} title="Run history" description="Every scenario execution, newest first." />
      <SearchToolbar name={state.name} labels={state.labels} onName={(value) => set('name', value)} onLabels={(value) => set('labels', value)} />
      {query.isPending && <Loading label="Loading run history" />}
      {query.isError && <ErrorState error={query.error} retry={() => query.refetch()} />}
      {query.data && query.data.data.length === 0 && <Empty title="No runs found" description="Runs appear here when a scenario is scheduled or triggered manually." />}
      {query.data && query.data.data.length > 0 && (
        <TableShell footer={<Pagination page={state.page} pageSize={state.pageSize || PAGE_SIZE} total={query.data.total} onPage={(page) => set('page', page)} />}>
          <RunTable runs={query.data.data} compareLink />
        </TableShell>
      )}
    </div>
  )
}

import {api} from '../api'
