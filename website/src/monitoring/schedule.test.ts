import {describe, expect, it} from 'vitest'
import {untilText} from './schedule'

describe('untilText', () => {
  const now = Date.parse('2026-10-04T12:00:00Z')
  it.each([
    [27 * 60_000 + 59_000, 'in 27 minutes'],
    [45_000, 'in 45 seconds'],
    [3 * 3_600_000 + 1, 'in 3 hours'],
    [26 * 3_600_000, 'tomorrow'],
    [0, 'due now'],
    [-90_000, 'due now'],
  ])('%i ms ahead reads %s', (ahead, text) => {
    expect(untilText(new Date(now + ahead), now)).toBe(text)
  })
})
