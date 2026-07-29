import {describe, expect, it} from 'vitest'
import type {Run, RunOutcome, Worker} from './types'
import {formatDuration, presenceLabel, runDuration, runSummary, workerCondition} from './utils'

const run = (name: string, result: RunOutcome, duration: number): Run => ({
  name,
  spec: {
    start_time: '2026-07-29T00:00:00Z',
    end_time: new Date(Date.parse('2026-07-29T00:00:00Z') + duration).toISOString(),
  },
  status: {status: 'completed', result},
})

describe('run measurements', () => {
  it('keeps lifecycle and outcome semantics while summarising', () => {
    const values = [run('ok', 'success', 1_000), run('bad', 'failed', 3_000), {name: 'queued', status: {status: 'pending'}} as Run]
    expect(runSummary(values)).toMatchObject({total: 3, settled: 2, successes: 1, successRate: 0.5, average: 2_000})
  })

  it('does not invent a duration for an unfinished run', () => {
    expect(runDuration({name: 'pending', spec: {start_time: '2026-07-29T00:00:00Z'}})).toBeNull()
    expect(formatDuration(null)).toBe('—')
  })
})

describe('presence', () => {
  it('uses the server verdict instead of recomputing from timestamps', () => {
    const worker = {metadata: {name: 'worker'}, spec: {}, status: {presence: {condition: 'api-unreachable'}}} as Worker
    expect(workerCondition(worker)).toBe('api-unreachable')
    expect(presenceLabel(workerCondition(worker))).toBe('no API contact')
  })
})
