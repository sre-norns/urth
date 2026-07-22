import {describe, it, expect} from 'vitest'
import {formatDuration, formatPercent, formatRelative, formatTimestamp} from './time.js'

describe('formatDuration', () => {
  it.each([
    [0, '0ms'],
    [27, '27ms'],
    [999, '999ms'],
    [1000, '1.0s'],
    [1500, '1.5s'],
    [9900, '9.9s'],
    [12000, '12s'],
    [65000, '1m 5s'],
    [120000, '2m'],
    [3600000, '1h'],
    [5400000, '1h 30m'],
  ])('renders %sms as %s', (ms, expected) => {
    expect(formatDuration(ms)).toBe(expected)
  })

  // A run that has not finished has no duration; an em dash says so, where "0ms"
  // would claim it finished instantly.
  it('renders an absent duration as a dash', () => {
    expect(formatDuration(null)).toBe('—')
    expect(formatDuration(undefined)).toBe('—')
    expect(formatDuration(NaN)).toBe('—')
  })
})

describe('formatRelative', () => {
  const now = new Date('2026-07-21T12:00:00Z')

  it.each([
    ['2026-07-21T11:59:55Z', 'just now'],
    ['2026-07-21T11:59:00Z', '1 minute ago'],
    ['2026-07-21T11:30:00Z', '30 minutes ago'],
    ['2026-07-21T09:00:00Z', '3 hours ago'],
    ['2026-07-19T12:00:00Z', '2 days ago'],
  ])('renders %s as %s', (value, expected) => {
    expect(formatRelative(value, now)).toBe(expected)
  })

  // The same helper renders the next scheduled run, which is in the future.
  it('renders future times forwards', () => {
    expect(formatRelative('2026-07-21T12:05:00Z', now)).toBe('in 5 minutes')
    expect(formatRelative('2026-07-21T13:00:00Z', now)).toBe('in 1 hour')
  })

  it('handles missing and invalid input', () => {
    expect(formatRelative(null, now)).toBe('never')
    expect(formatRelative('not-a-date', now)).toBe('unknown')
  })

  it('falls back to a date beyond a month', () => {
    expect(formatRelative('2026-01-01T12:00:00Z', now)).toMatch(/\d/)
  })
})

describe('formatPercent', () => {
  it.each([
    [1, '100%'],
    [0, '0%'],
    [0.5, '50%'],
    [2 / 3, '67%'],
  ])('renders %s as %s', (fraction, expected) => {
    expect(formatPercent(fraction)).toBe(expected)
  })

  // Rounding 99.6% to 100% would tell an operator nothing ever failed, when
  // something did. That is the one case worth a decimal place.
  it('does not round a near miss up to a clean 100%', () => {
    expect(formatPercent(0.996)).toBe('99.6%')
    expect(formatPercent(0.999)).toBe('99.9%')
  })

  it('renders an unknown rate as a dash', () => {
    expect(formatPercent(null)).toBe('—')
  })
})

describe('formatTimestamp', () => {
  it('renders a dash when there is no value', () => {
    expect(formatTimestamp(null)).toBe('—')
  })

  it('renders unparseable input as unknown', () => {
    expect(formatTimestamp('not-a-date')).toBe('unknown')
  })
})
