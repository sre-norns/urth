import {useState} from 'react'
import {useClock} from '@sre-norns/components'
import {useIdentity} from '@sre-norns/components/identity'
import {useNavigate, useParams} from 'react-router-dom'
import {request} from '../api/http'
import {placement, run, type Scenario} from '../api/models'
import {resourcePath} from '../api/queries'
import {projectPath} from '../identity/links'

const units = [['day', 86_400], ['hour', 3_600], ['minute', 60], ['second', 1]] as const

/**
 * The server recomputes the next run on every read, so a time already past
 * means the list has not been polled since -- not that a run was missed.
 */
export function untilText(next: Date, now: number) {
  const seconds = Math.floor((next.getTime() - now) / 1000)
  if (seconds <= 0) return 'due now'
  const [unit, size] = units.find(([, size]) => seconds >= size) ?? units[3]
  return new Intl.RelativeTimeFormat(undefined, {numeric: 'auto'}).format(Math.floor(seconds / size), unit)
}

/** The next run is the server's: it evaluates the cron dialect the scheduler uses. */
export function NextRun({scenario}: {scenario: Scenario}) {
  const now = useClock()
  const next = scenario.status?.nextScheduledRunTime
  if (!scenario.spec.schedule) return <span>Manual</span>
  return <span className="last-seen">
    <code>{scenario.spec.schedule}</code>
    {!scenario.spec.active ? <small>Scheduling disabled</small>
      : next ? <><time dateTime={next}>{untilText(new Date(next), now)}</time><small>{new Date(next).toLocaleString()}</small></>
        : <small>Not scheduled</small>}
  </span>
}

/** The server refuses runs of inactive scenarios and of scenarios without a probe. */
export function runnable(scenario: Scenario) {
  return scenario.spec.active && Boolean(scenario.spec.prob?.kind)
}

/**
 * Placement is checked when the run is requested, not ahead of time, so a list
 * of scenarios costs one placement query per click rather than one per row
 * per poll -- and the answer is never older than the run it gates.
 */
export function useRunNow() {
  const {accountId = '', projectId = ''} = useParams()
  const {session, queryClient} = useIdentity()
  const navigate = useNavigate()
  const [pending, setPending] = useState<string>()
  const [error, setError] = useState<unknown>()
  async function runNow(name: string) {
    const path = resourcePath('projects', projectId, 'scenarios', name)
    setPending(name); setError(undefined)
    try {
      const check = await request(session, `${path}/placement`, placement)
      if (!check.data.schedulable) throw new Error(check.data.detail || check.data.reason || 'No eligible runner is available.')
      const created = await request(session, `${path}/results`, run, {method: 'POST', body: {apiVersion: 'urth.sre-norns.com/v1', kind: 'results', metadata: {labels: {trigger: 'manual', triggerAgent: 'website'}}, spec: {}}})
      void queryClient.invalidateQueries({queryKey: [accountId, projectId]})
      void navigate(`${projectPath(accountId, projectId)}/runs/${encodeURIComponent(created.data.metadata.name)}`)
    } catch (error) {setError(error)} finally {setPending(undefined)}
  }
  return {runNow, pending, error}
}
