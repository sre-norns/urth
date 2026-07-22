import {describe, it, expect} from 'vitest'
import {
  Period,
  filterByPeriod,
  isSettled,
  isSuccess,
  runDurationMs,
  periodQuery,
  runStartedAt,
  summariseRuns,
} from './runStats.js'

const run = ({result = 'success', start, end, created} = {}) => ({
  name: 'run',
  creationTimestamp: created,
  spec: {start_time: start, end_time: end},
  status: {status: 'completed', result},
})

describe('runDurationMs', () => {
  it('measures the probe, start to finish', () => {
    expect(runDurationMs(run({start: '2026-07-21T10:00:00Z', end: '2026-07-21T10:00:01.500Z'}))).toBe(1500)
  })

  // An unfinished run is not a zero-length run, and rendering it as 0ms would
  // quietly drag an average down.
  it('is null while a run is still in flight', () => {
    expect(runDurationMs(run({start: '2026-07-21T10:00:00Z'}))).toBeNull()
  })

  it('is null when timestamps are missing or unparseable', () => {
    expect(runDurationMs(run())).toBeNull()
    expect(runDurationMs(run({start: 'not-a-date', end: 'also-not'}))).toBeNull()
    expect(runDurationMs(undefined)).toBeNull()
  })
})

describe('runStartedAt', () => {
  // A run can wait in the queue before a worker claims it; the wait is not part
  // of the run, so the probe's own start time wins where it exists.
  it('prefers the probe start over the resource creation time', () => {
    const value = runStartedAt(run({start: '2026-07-21T10:00:00Z', created: '2026-07-21T09:00:00Z'}))
    expect(value.toISOString()).toBe('2026-07-21T10:00:00.000Z')
  })

  it('falls back to creation time for a run that never started', () => {
    const value = runStartedAt(run({created: '2026-07-21T09:00:00Z'}))
    expect(value.toISOString()).toBe('2026-07-21T09:00:00.000Z')
  })
})

describe('outcome predicates', () => {
  it('reads success from the probe outcome, not the job state', () => {
    expect(isSuccess({status: {status: 'completed', result: 'success'}})).toBe(true)
    // The job completed; the probe did not like what it found.
    expect(isSuccess({status: {status: 'completed', result: 'failed'}})).toBe(false)
  })

  it('treats a run with no outcome as unsettled', () => {
    expect(isSettled({status: {status: 'running'}})).toBe(false)
    expect(isSettled({status: {status: 'completed', result: 'errored'}})).toBe(true)
  })
})

describe('filterByPeriod', () => {
  const now = new Date('2026-07-21T12:00:00Z')
  const runs = [
    run({start: '2026-07-21T11:00:00Z'}), // an hour ago
    run({start: '2026-07-19T12:00:00Z'}), // two days ago
    run({start: '2026-06-01T12:00:00Z'}), // seven weeks ago
  ]

  it('keeps runs inside the window', () => {
    expect(filterByPeriod(runs, Period.Day, now)).toHaveLength(1)
    expect(filterByPeriod(runs, Period.Week, now)).toHaveLength(2)
    expect(filterByPeriod(runs, Period.Month, now)).toHaveLength(2)
  })

  it('keeps everything for all time', () => {
    expect(filterByPeriod(runs, Period.All, now)).toHaveLength(3)
  })

  it('survives an unknown period and empty input', () => {
    expect(filterByPeriod(runs, 'nonsense', now)).toHaveLength(3)
    expect(filterByPeriod(undefined, Period.Day, now)).toEqual([])
  })
})

describe('summariseRuns', () => {
  it('counts outcomes and averages duration', () => {
    const summary = summariseRuns([
      run({result: 'success', start: '2026-07-21T10:00:00Z', end: '2026-07-21T10:00:01Z'}),
      run({result: 'success', start: '2026-07-21T10:01:00Z', end: '2026-07-21T10:01:03Z'}),
      run({result: 'failed', start: '2026-07-21T10:02:00Z', end: '2026-07-21T10:02:02Z'}),
    ])

    expect(summary.total).toBe(3)
    expect(summary.succeeded).toBe(2)
    expect(summary.failed).toBe(1)
    expect(summary.successRate).toBeCloseTo(2 / 3)
    expect(summary.averageDurationMs).toBe(2000)
  })

  // A run still in flight is not a failure. Counting it as one would make a
  // scenario look broken for as long as it takes to finish.
  it('excludes unsettled runs from the success rate', () => {
    const summary = summariseRuns([
      run({result: 'success', start: '2026-07-21T10:00:00Z', end: '2026-07-21T10:00:01Z'}),
      {name: 'in-flight', spec: {start_time: '2026-07-21T10:01:00Z'}, status: {status: 'running'}},
    ])

    expect(summary.total).toBe(2)
    expect(summary.settled).toBe(1)
    expect(summary.successRate).toBe(1)
  })

  // Reporting 0% when nothing has run implies everything failed.
  it('reports no success rate when nothing has settled', () => {
    expect(summariseRuns([]).successRate).toBeNull()
    expect(summariseRuns([]).averageDurationMs).toBeNull()
    expect(summariseRuns(undefined).total).toBe(0)
  })

  it('identifies the most recent run regardless of input order', () => {
    const summary = summariseRuns([
      run({start: '2026-07-21T10:00:00Z'}),
      run({start: '2026-07-21T12:00:00Z'}),
      run({start: '2026-07-21T11:00:00Z'}),
    ])

    expect(runStartedAt(summary.lastRun).toISOString()).toBe('2026-07-21T12:00:00.000Z')
  })
})

describe('periodQuery', () => {
  const now = new Date('2026-07-21T12:00:00Z')

  it('asks the server for the window rather than fetching everything', () => {
    expect(periodQuery(Period.Day, now).get('from')).toBe('2026-07-20T12:00:00Z')
    expect(periodQuery(Period.Week, now).get('from')).toBe('2026-07-14T12:00:00Z')
  })

  // The server's date parser rejects fractional seconds with a 400. Sending
  // toISOString() verbatim broke run history for every period but All time.
  it('sends whole seconds, which is all the server accepts', () => {
    for (const period of [Period.Day, Period.Week, Period.Month]) {
      expect(periodQuery(period, now).get('from')).not.toMatch(/\.\d+Z$/)
      expect(periodQuery(period, now).get('from')).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/)
    }
  })

  it('sends no bound for all time', () => {
    expect(periodQuery(Period.All, now).has('from')).toBe(false)
  })

  it('falls back to all time for an unknown period', () => {
    expect(periodQuery('nonsense', now).has('from')).toBe(false)
  })
})
