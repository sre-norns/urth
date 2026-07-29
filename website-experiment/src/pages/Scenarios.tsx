import {useMemo, useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {ChevronDown, Plus, Radio} from 'lucide-react'
import {Link} from 'react-router-dom'
import {api} from '../api'
import {Heartbeat, Pagination, TableShell} from '../components/Data'
import {SearchToolbar} from '../components/SearchToolbar'
import {Empty, ErrorState, LabelChips, LinkButton, Loading, PageHeader, Status} from '../components/ui'
import {useListSearch} from '../hooks'
import type {Scenario} from '../types'
import {displayOutcome, formatRelative, PAGE_SIZE} from '../utils'

function scenarioState(scenario: Scenario) {
  if (!scenario.spec.active) return 'paused'
  const run = scenario.status?.results?.[0]
  return run ? displayOutcome(run) : 'new'
}

export function ScenariosPage() {
  const {state, set} = useListSearch()
  const [groupBy, setGroupBy] = useState('')
  const query = useQuery({
    queryKey: ['scenarios', state],
    queryFn: () => api.scenarios.list({name: state.name, labels: state.labels, page: state.page, pageSize: state.pageSize}),
    refetchInterval: 30_000,
  })
  const scenarios = query.data?.data ?? []
  const summary = useMemo(
    () => scenarios.reduce((result, scenario) => {
      const status = scenarioState(scenario)
      result[status] = (result[status] || 0) + 1
      return result
    }, {} as Record<string, number>),
    [scenarios],
  )
  const labelKeys = useMemo(
    () => Array.from(new Set(scenarios.flatMap((scenario) => Object.keys(scenario.metadata.labels ?? {}).filter((key) => !key.startsWith('urth/'))))).sort(),
    [scenarios],
  )
  const groups = useMemo(() => {
    if (!groupBy) return [['', scenarios]] as Array<[string, Scenario[]]>
    return Array.from(
      scenarios.reduce((map, scenario) => {
        const value = scenario.metadata.labels?.[groupBy] || 'unlabelled'
        map.set(value, [...(map.get(value) ?? []), scenario])
        return map
      }, new Map<string, Scenario[]>()),
    )
  }, [groupBy, scenarios])

  return (
    <div className="page">
      <PageHeader
        eyebrow={<><Radio size={13} /> Monitor</>}
        title="Scenarios"
        description="Repeatable checks against the systems you care about."
        actions={<LinkButton to="/scenarios/new"><Plus size={16} /> New scenario</LinkButton>}
      />
      <div className="summary-line" aria-label="Status summary for this page">
        <span><strong>{query.data?.total ?? 0}</strong> total</span>
        {Object.entries(summary).map(([status, count]) => <Status key={status} value={`${count} ${status}`} subtle />)}
        <em>Summary reflects this page</em>
      </div>
      <SearchToolbar
        name={state.name}
        labels={state.labels}
        onName={(value) => set('name', value)}
        onLabels={(value) => set('labels', value)}
      >
        <label className="select-control">
          <span>Group by</span>
          <select value={groupBy} onChange={(event) => setGroupBy(event.target.value)}>
            <option value="">None</option>
            {labelKeys.map((key) => <option key={key} value={key}>{key}</option>)}
          </select>
          <ChevronDown size={14} />
        </label>
      </SearchToolbar>
      {query.isPending && <Loading label="Loading scenarios" />}
      {query.isError && <ErrorState error={query.error} retry={() => query.refetch()} />}
      {!query.isPending && !query.isError && scenarios.length === 0 && (
        <Empty title="No scenarios found" description="Adjust the filters or define the first synthetic check." action={<LinkButton to="/scenarios/new">Create scenario</LinkButton>} />
      )}
      {groups.map(([group, items]) => items.length > 0 && (
        <section className="resource-group" key={group || 'all'}>
          {group && <div className="group-heading"><h2>{group}</h2><span>{items.length} on this page</span></div>}
          <TableShell>
            <div className="table-scroll">
              <table>
                <thead><tr><th>Scenario</th><th>State</th><th>Schedule</th><th>Last run</th><th>Recent history</th><th>Labels</th></tr></thead>
                <tbody>
                  {items.map((scenario) => {
                    const latest = scenario.status?.results?.[0]
                    return (
                      <tr key={scenario.metadata.uid || scenario.metadata.name}>
                        <td>
                          <Link className="table-primary" to={`/scenarios/${encodeURIComponent(scenario.metadata.name)}`}>{scenario.metadata.name}</Link>
                          <span className="table-secondary">{scenario.spec.description || scenario.spec.prob?.kind || 'No description'}</span>
                        </td>
                        <td><Status value={scenarioState(scenario)} /></td>
                        <td><span className="mono">{scenario.spec.schedule || 'manual only'}</span><span className="table-secondary">{scenario.status?.nextScheduledRunTime ? `next ${formatRelative(scenario.status.nextScheduledRunTime)}` : 'not scheduled'}</span></td>
                        <td>{latest ? <><span className="table-primary">{formatRelative(latest.spec?.start_time || latest.creationTimestamp)}</span><span className="table-secondary">{displayOutcome(latest)}</span></> : <span className="muted">Never run</span>}</td>
                        <td><Heartbeat runs={scenario.status?.results} /></td>
                        <td><LabelChips labels={scenario.metadata.labels} onClick={(key, value) => set('labels', `${key} = ${value}`)} /></td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
            <Pagination page={state.page} pageSize={state.pageSize || PAGE_SIZE} total={query.data?.total ?? 0} onPage={(page) => set('page', page)} />
          </TableShell>
        </section>
      ))}
    </div>
  )
}
