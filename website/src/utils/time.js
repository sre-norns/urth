// formatDuration renders a millisecond span the way an operator reads it: sub
// second probes in milliseconds, longer ones in the largest unit that still
// carries information.
export const formatDuration = (ms) => {
  if (ms === null || ms === undefined || Number.isNaN(ms)) {
    return '—'
  }

  if (ms < 1000) {
    return `${Math.round(ms)}ms`
  }

  const seconds = ms / 1000
  if (seconds < 60) {
    // Two significant figures below ten seconds, where the difference between
    // 1.2s and 1.9s matters; whole seconds above it, where it does not.
    return seconds < 10 ? `${seconds.toFixed(1)}s` : `${Math.round(seconds)}s`
  }

  const minutes = Math.floor(seconds / 60)
  const remainder = Math.round(seconds % 60)
  if (minutes < 60) {
    return remainder ? `${minutes}m ${remainder}s` : `${minutes}m`
  }

  const hours = Math.floor(minutes / 60)
  const minuteRemainder = minutes % 60
  return minuteRemainder ? `${hours}h ${minuteRemainder}m` : `${hours}h`
}

const UNITS = [
  { limit: 60 * 1000, ms: 1000, name: 'second' },
  { limit: 60 * 60 * 1000, ms: 60 * 1000, name: 'minute' },
  { limit: 24 * 60 * 60 * 1000, ms: 60 * 60 * 1000, name: 'hour' },
  { limit: 30 * 24 * 60 * 60 * 1000, ms: 24 * 60 * 60 * 1000, name: 'day' },
]

// formatRelative renders a time as a distance from now, in both directions --
// run history looks backwards, the next scheduled run looks forwards.
export const formatRelative = (value, now = new Date()) => {
  if (!value) {
    return 'never'
  }

  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) {
    return 'unknown'
  }

  const delta = date.getTime() - now.getTime()
  const magnitude = Math.abs(delta)

  if (magnitude < 10 * 1000) {
    return 'just now'
  }

  const unit = UNITS.find((u) => magnitude < u.limit)
  if (!unit) {
    return date.toLocaleDateString()
  }

  const count = Math.round(magnitude / unit.ms)
  const plural = count === 1 ? unit.name : `${unit.name}s`

  return delta < 0 ? `${count} ${plural} ago` : `in ${count} ${plural}`
}

export const formatTimestamp = (value) => {
  if (!value) {
    return '—'
  }

  const date = value instanceof Date ? value : new Date(value)
  return Number.isNaN(date.getTime()) ? 'unknown' : date.toLocaleString()
}

export const formatPercent = (fraction) => {
  if (fraction === null || fraction === undefined || Number.isNaN(fraction)) {
    return '—'
  }

  const percent = fraction * 100
  // Avoid rounding 99.6% up to a clean 100%, which would read as "nothing ever
  // failed" when something did.
  if (percent > 99 && percent < 100) {
    return `${percent.toFixed(1)}%`
  }

  return `${Math.round(percent)}%`
}
