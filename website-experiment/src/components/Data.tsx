import type {ReactNode} from 'react'
import {ChevronLeft, ChevronRight} from 'lucide-react'
import {Link} from 'react-router-dom'
import {labels, runScenarioName} from '../labels'
import type {Run} from '../types'
import {displayOutcome, formatDuration, formatRelative, formatTimestamp, runDuration} from '../utils'
import {Status} from './ui'

export function Heartbeat({runs, limit = 36}: {runs?: Run[]; limit?: number}) {
  const items = (runs ?? []).slice(0, limit).reverse()
  const blanks = Math.max(0, limit - items.length)
  return (
    <div className="heartbeat" aria-label={`${items.length} recent run statuses`}>
      {Array.from({length: blanks}).map((_, index) => <i className="heartbeat-empty" key={`empty-${index}`} />)}
      {items.map((run) => (
        <i
          className={`heartbeat-${displayOutcome(run)}`}
          key={run.uid || run.name}
          title={`${displayOutcome(run)} · ${formatTimestamp(run.spec?.start_time || run.creationTimestamp)}`}
        />
      ))}
    </div>
  )
}

export function RunTable({
  runs,
  scenarioName,
  compareLink,
}: {
  runs: Run[]
  scenarioName?: string
  compareLink?: boolean
}) {
  return (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Run</th>
            {!scenarioName && <th>Scenario</th>}
            <th>Outcome</th>
            <th>Lifecycle</th>
            <th>Started</th>
            <th>Duration</th>
            <th>Runner / worker</th>
            <th className="align-right">Artifacts</th>
          </tr>
        </thead>
        <tbody>
          {runs.map((run) => {
            const scenario = scenarioName || runScenarioName(run)
            const runPath = scenario
              ? `/scenarios/${encodeURIComponent(scenario)}/runs/${encodeURIComponent(run.name)}`
              : `/runs/${encodeURIComponent(run.name)}`
            return (
              <tr key={run.uid || run.name}>
                <td>
                  <Link className="mono table-primary" to={runPath}>{run.name}</Link>
                  {compareLink && scenario && <Link className="table-secondary" to={`${runPath}?compare=previous`}>Compare</Link>}
                </td>
                {!scenarioName && <td>{scenario ? <Link to={`/scenarios/${encodeURIComponent(scenario)}`}>{scenario}</Link> : <span className="muted">historical</span>}</td>}
                <td><Status run={run} /></td>
                <td><span className="mono">{run.status?.status || 'unknown'}</span></td>
                <td>
                  <span className="table-primary">{formatRelative(run.spec?.start_time || run.creationTimestamp)}</span>
                  <span className="table-secondary mono">{formatTimestamp(run.spec?.start_time || run.creationTimestamp)}</span>
                </td>
                <td className="mono">{formatDuration(runDuration(run))}</td>
                <td>
                  {run.status?.executor?.runnerName ? (
                    <Link className="table-primary" to={`/runners/${encodeURIComponent(run.status.executor.runnerName)}`}>{run.status.executor.runnerName}</Link>
                  ) : <span className="muted">—</span>}
                  {run.status?.executor?.workerName && (
                    <Link className="table-secondary" to={`/workers/${encodeURIComponent(run.status.executor.workerName)}`}>{run.status.executor.workerName}</Link>
                  )}
                </td>
                <td className="align-right mono">{run.status?.numberArtifacts ?? 0}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

export function Pagination({
  page,
  pageSize,
  total,
  onPage,
}: {
  page: number
  pageSize: number
  total: number
  onPage: (page: number) => void
}) {
  const pages = Math.max(1, Math.ceil(total / pageSize))
  return (
    <div className="pagination">
      <span>Showing {total ? page * pageSize + 1 : 0}–{Math.min((page + 1) * pageSize, total)} of {total}</span>
      <div>
        <button className="icon-button" aria-label="Previous page" disabled={page <= 0} onClick={() => onPage(page - 1)}><ChevronLeft size={17} /></button>
        <span>Page {page + 1} of {pages}</span>
        <button className="icon-button" aria-label="Next page" disabled={page >= pages - 1} onClick={() => onPage(page + 1)}><ChevronRight size={17} /></button>
      </div>
    </div>
  )
}

export function TableShell({children, footer}: {children: ReactNode; footer?: ReactNode}) {
  return <div className="table-shell">{children}{footer}</div>
}

export function artifactResultName(value?: {metadata?: {labels?: Record<string, string>}}) {
  return value?.metadata?.labels?.[labels.artifact.resultName]
}
