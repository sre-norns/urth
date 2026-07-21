// Vocabulary note: a scenario's execution is a "run", and the API calls the
// resource a "result". A run carries two separate outcomes:
//
//   status.status  -- how the job itself went (pending, running, completed, errored)
//   status.result  -- what the probe concluded (success, failed, errored, timeout, canceled)
//
// A run can be `completed` as a job while its probe reported `failed`, so
// success has to be read from `result`, not from the job state.

export const RunResult = {
  Success: 'success',
  Failed: 'failed',
  Errored: 'errored',
  Timeout: 'timeout',
  Canceled: 'canceled',
}

export const JobStatus = {
  Pending: 'pending',
  Running: 'running',
  Completed: 'completed',
  Errored: 'errored',
}

export const Period = {
  Day: '24h',
  Week: '7d',
  Month: '30d',
  All: 'all',
}

export const PERIODS = [
  { id: Period.Day, label: '24 hours', ms: 24 * 60 * 60 * 1000 },
  { id: Period.Week, label: '7 days', ms: 7 * 24 * 60 * 60 * 1000 },
  { id: Period.Month, label: '30 days', ms: 30 * 24 * 60 * 60 * 1000 },
  { id: Period.All, label: 'All time', ms: null },
]

// runStartedAt prefers the moment the probe actually began over the moment the
// run resource was created: a run can sit in the queue for a while before a
// worker claims it, and the queue wait is not part of the run.
export const runStartedAt = (run) => {
  const started = run?.spec?.start_time || run?.creationTimestamp
  if (!started) {
    return null
  }

  const date = new Date(started)
  return Number.isNaN(date.getTime()) ? null : date
}

export const runFinishedAt = (run) => {
  const ended = run?.spec?.end_time
  if (!ended) {
    return null
  }

  const date = new Date(ended)
  return Number.isNaN(date.getTime()) ? null : date
}

// runDurationMs returns how long the probe took, or null when the run has not
// finished -- which is different from a run that took no measurable time.
export const runDurationMs = (run) => {
  const started = runStartedAt(run)
  const ended = runFinishedAt(run)
  if (!started || !ended) {
    return null
  }

  const duration = ended.getTime() - started.getTime()
  return duration >= 0 ? duration : null
}

export const isSuccess = (run) => run?.status?.result === RunResult.Success

// isSettled reports whether a run has reached an outcome. An unsettled run is
// excluded from success rates, so that a run still in flight does not read as a
// failure while it is waiting.
export const isSettled = (run) => Boolean(run?.status?.result)

export const periodById = (id) => PERIODS.find((p) => p.id === id) || PERIODS[PERIODS.length - 1]

export const filterByPeriod = (runs, periodId, now = new Date()) => {
  const period = periodById(periodId)
  if (!period.ms) {
    return runs || []
  }

  const cutoff = now.getTime() - period.ms
  return (runs || []).filter((run) => {
    const started = runStartedAt(run)
    return started ? started.getTime() >= cutoff : false
  })
}

// summariseRuns reduces a set of runs to the figures shown above a scenario's
// history. successRate is null rather than 0 when nothing has settled, so the
// UI can say "no data" instead of implying a 0% success rate.
export const summariseRuns = (runs) => {
  const all = runs || []
  const settled = all.filter(isSettled)
  const succeeded = settled.filter(isSuccess)
  const durations = all.map(runDurationMs).filter((d) => d !== null)

  return {
    total: all.length,
    settled: settled.length,
    succeeded: succeeded.length,
    failed: settled.length - succeeded.length,
    successRate: settled.length ? succeeded.length / settled.length : null,
    averageDurationMs: durations.length
      ? Math.round(durations.reduce((sum, d) => sum + d, 0) / durations.length)
      : null,
    lastRun: all.reduce((latest, run) => {
      const started = runStartedAt(run)
      if (!started) {
        return latest
      }
      const latestStarted = runStartedAt(latest)
      return !latestStarted || started > latestStarted ? run : latest
    }, null),
  }
}
