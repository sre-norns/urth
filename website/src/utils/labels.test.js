import { describe, it, expect } from 'vitest'
import { isSystemLabel, splitLabels } from './labels.js'

describe('isSystemLabel', () => {
  it.each([
    ['urth/scenario.kind', true],
    ['urth/artifact.data-class', true],
    ['team', false],
    ['app.kubernetes.io/name', false],
    ['', false],
    [undefined, false],
  ])('%s -> %s', (key, expected) => {
    expect(isSystemLabel(key)).toBe(expected)
  })
})

describe('splitLabels', () => {
  // The server recomputes its own labels on every save, so editing one has no
  // lasting effect. Separating them keeps the editor to what a user can change.
  it('separates server-owned labels from the user\'s own', () => {
    const { user, system } = splitLabels({
      team: 'checkout',
      env: 'prod',
      'urth/scenario.kind': 'http',
      'urth/scenario.name': 'my-probe',
    })

    expect(user).toEqual({ team: 'checkout', env: 'prod' })
    expect(system).toEqual({ 'urth/scenario.kind': 'http', 'urth/scenario.name': 'my-probe' })
  })

  it('handles missing labels', () => {
    expect(splitLabels(undefined)).toEqual({ user: {}, system: {} })
  })
})
