import {describe, it, expect} from 'vitest'
import {UNRESOLVED_SELECTOR, deadLetterRows, hasResolvedFilter, withUnresolvedFilter} from './deadLetters.js'

describe('withUnresolvedFilter', () => {
  // The default view answers "what is still broken". Without the selector the
  // list quietly widens to every failure ever recorded.
  it('limits an empty query to unresolved failures', () => {
    const params = new URLSearchParams(withUnresolvedFilter(''))

    expect(params.get('labels')).toBe(UNRESOLVED_SELECTOR)
  })

  it('appends to an existing label query rather than replacing it', () => {
    const params = new URLSearchParams(withUnresolvedFilter('labels=urth%2Frunner.name+%3D+r1'))

    expect(params.get('labels')).toBe(`urth/runner.name = r1,${UNRESOLVED_SELECTOR}`)
  })

  // `all` is a view flag the API does not understand, so it must be translated
  // and then dropped rather than forwarded.
  it('drops the view flag and the filter when showing everything', () => {
    const params = new URLSearchParams(withUnresolvedFilter('all=true'))

    expect(params.has('all')).toBe(false)
    expect(params.has('labels')).toBe(false)
  })

  it('keeps other parameters untouched', () => {
    const params = new URLSearchParams(withUnresolvedFilter('from=2026-07-01&all=true'))

    expect(params.get('from')).toBe('2026-07-01')
  })
})

describe('hasResolvedFilter', () => {
  it('is false by default and true once asked for', () => {
    expect(hasResolvedFilter('')).toBe(false)
    expect(hasResolvedFilter('all=true')).toBe(true)
  })
})

describe('deadLetterRows', () => {
  // A dispatch failure is a normal resource, with name and labels under
  // `metadata` -- unlike a run result, which is flat. Reading it as if it were
  // flat is the mistake this guards.
  it('reads identity from metadata and detail from spec/status', () => {
    const rows = deadLetterRows({
      data: [
        {
          metadata: {name: 'dispatch-1', labels: {'urth/runner.name': 'r1'}},
          spec: {reason: 'misrouted-dispatch', scenarioName: 'probe', deliveries: 3, reportedBy: 'worker'},
          status: {resolved: true, retryResultName: 'run-abc'},
        },
      ],
    })

    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({
      name: 'dispatch-1',
      reason: 'misrouted-dispatch',
      scenarioName: 'probe',
      runnerName: 'r1',
      deliveries: 3,
      resolved: true,
      retryResultName: 'run-abc',
    })
  })

  it('tolerates a response with no data', () => {
    expect(deadLetterRows(undefined)).toEqual([])
    expect(deadLetterRows({})).toEqual([])
  })

  it('falls back rather than rendering undefined', () => {
    const rows = deadLetterRows({data: [{metadata: {name: 'd'}}]})

    expect(rows[0].reason).toBe('unknown')
    expect(rows[0].runnerName).toBe('')
    expect(rows[0].resolved).toBe(false)
  })
})
