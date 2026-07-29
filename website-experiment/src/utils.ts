import type {LabelSelector, PresenceCondition, Run, RunOutcome, Worker} from './types'

export const REFRESH_MS = 15_000
export const PAGE_SIZE = 25

export function formatTimestamp(value?: string | Date): string {
  if (!value) return '—'
  const date = value instanceof Date ? value : new Date(value)
  return Number.isNaN(date.getTime()) ? 'unknown' : date.toLocaleString()
}

export function formatRelative(value?: string | Date, now = new Date()): string {
  if (!value) return 'never'
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return 'unknown'
  const seconds = Math.round((date.getTime() - now.getTime()) / 1000)
  const formatter = new Intl.RelativeTimeFormat(undefined, {numeric: 'auto'})
  if (Math.abs(seconds) < 60) return formatter.format(seconds, 'second')
  const minutes = Math.round(seconds / 60)
  if (Math.abs(minutes) < 60) return formatter.format(minutes, 'minute')
  const hours = Math.round(minutes / 60)
  if (Math.abs(hours) < 24) return formatter.format(hours, 'hour')
  const days = Math.round(hours / 24)
  if (Math.abs(days) < 30) return formatter.format(days, 'day')
  return date.toLocaleDateString()
}

export function runDuration(run?: Run): number | null {
  const start = run?.spec?.start_time
  const end = run?.spec?.end_time
  if (!start || !end) return null
  const duration = new Date(end).getTime() - new Date(start).getTime()
  return Number.isFinite(duration) && duration >= 0 ? duration : null
}

export function formatDuration(value?: number | null): string {
  if (value === null || value === undefined || Number.isNaN(value)) return '—'
  if (value < 1000) return `${Math.round(value)}ms`
  if (value < 60_000) return value < 10_000 ? `${(value / 1000).toFixed(1)}s` : `${Math.round(value / 1000)}s`
  const minutes = Math.floor(value / 60_000)
  const seconds = Math.round((value % 60_000) / 1000)
  return seconds ? `${minutes}m ${seconds}s` : `${minutes}m`
}

export function displayOutcome(run?: Run): string {
  return run?.status?.result || run?.status?.status || 'unknown'
}

export function statusTone(value?: Run | RunOutcome | PresenceCondition | string): string {
  const status =
    typeof value === 'object' ? value?.status?.result || value?.status?.status || 'unknown' : value
  if (status === 'success' || status === 'online' || status === 'active') return 'success'
  if (status === 'failed' || status === 'errored' || status === 'offline') return 'critical'
  if (
    status === 'timeout' ||
    status === 'canceled' ||
    status === 'api-unreachable' ||
    status === 'nats-unreachable'
  )
    return 'warning'
  if (status === 'running') return 'info'
  return 'neutral'
}

export function workerCondition(worker?: Worker): PresenceCondition {
  return worker?.status?.presence?.condition || 'unknown'
}

export function latestSeen(worker?: Worker): string | undefined {
  const values = [worker?.status?.lastSeenTime, worker?.status?.natsLastSeenTime]
    .filter(Boolean)
    .map((value) => new Date(value!).getTime())
    .filter(Number.isFinite)
  return values.length ? new Date(Math.max(...values)).toISOString() : undefined
}

export function presenceLabel(condition: PresenceCondition): string {
  return (
    {
      online: 'online',
      offline: 'offline',
      unknown: 'presence unknown',
      'api-unreachable': 'no API contact',
      'nats-unreachable': 'not on its queue',
    } as Record<PresenceCondition, string>
  )[condition]
}

export function requirementLines(selector?: LabelSelector): string[] {
  const labels = Object.entries(selector?.matchLabels ?? {}).map(([key, value]) => `${key} = ${value}`)
  const expressions = selector?.matchExpressions ?? selector?.matchSelector ?? []
  return labels.concat(
    expressions.map((entry) =>
      entry.values?.length
        ? `${entry.key} ${entry.operator} (${entry.values.join(',')})`
        : `${entry.key} ${entry.operator}`,
    ),
  )
}

export function runSummary(runs: Run[]) {
  const settled = runs.filter((run) => Boolean(run.status?.result))
  const successes = settled.filter((run) => run.status?.result === 'success')
  const durations = runs.map(runDuration).filter((value): value is number => value !== null)
  return {
    total: runs.length,
    successRate: settled.length ? successes.length / settled.length : null,
    settled: settled.length,
    successes: successes.length,
    average: durations.length
      ? Math.round(durations.reduce((sum, duration) => sum + duration, 0) / durations.length)
      : null,
  }
}

export function formatPercent(value: number | null): string {
  if (value === null) return '—'
  const percent = value * 100
  return percent > 99 && percent < 100 ? `${percent.toFixed(1)}%` : `${Math.round(percent)}%`
}
