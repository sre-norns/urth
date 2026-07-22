import {describe, it, expect} from 'vitest'
import {Operator, Rule, Selector, parseSelectorExpression, stringify} from './k8s-labels.js'

describe('Rule.toString', () => {
  const cases = [
    [new Rule('env', Operator.Equals, ['prod']), 'env = prod'],
    [new Rule('env', Operator.NotEquals, ['dev']), 'env != dev'],
    [new Rule('tier', Operator.In, ['web', 'api']), 'tier in (web,api)'],
    [new Rule('tier', Operator.NotIn, ['dev', 'testing']), 'tier notin (dev,testing)'],
    [new Rule('team', Operator.Exists), 'team'],
    [new Rule('team', Operator.DoesNotExist), '!team'],
    [new Rule('node.major', Operator.GreaterThen, ['18']), 'node.major > 18'],
    [new Rule('node.major', Operator.LessThen, ['22']), 'node.major < 22'],
  ]

  it.each(cases)('renders %s', (rule, expected) => {
    expect(rule.toString()).toBe(expected)
  })
})

describe('parseSelectorExpression', () => {
  it('parses an equality rule', () => {
    expect(parseSelectorExpression('env = prod')).toEqual([new Rule('env', Operator.Equals, ['prod'])])
  })

  it('parses an inequality rule', () => {
    expect(parseSelectorExpression('env != dev')).toEqual([new Rule('env', Operator.NotEquals, ['dev'])])
  })

  it('parses set membership', () => {
    expect(parseSelectorExpression('tier in (web,api)')).toEqual([new Rule('tier', Operator.In, ['web', 'api'])])
  })

  it('parses set exclusion', () => {
    expect(parseSelectorExpression('env notin (dev,testing)')).toEqual([
      new Rule('env', Operator.NotIn, ['dev', 'testing']),
    ])
  })

  it('parses existence and non-existence', () => {
    expect(parseSelectorExpression('team')).toEqual([new Rule('team', Operator.Exists, [])])
    expect(parseSelectorExpression('!team')).toEqual([new Rule('team', Operator.DoesNotExist, [])])
  })

  // Commas inside a value list must not split the expression.
  it('splits on commas outside value lists only', () => {
    expect(parseSelectorExpression('env notin (dev,testing),tier = web')).toEqual([
      new Rule('env', Operator.NotIn, ['dev', 'testing']),
      new Rule('tier', Operator.Equals, ['web']),
    ])
  })

  it('rejects an unknown operator', () => {
    expect(() => parseSelectorExpression('env ~ prod')).toThrow(/Invalid expression/)
  })
})

describe('round trip', () => {
  const expressions = ['env = prod', 'env != dev', 'tier in (web,api)', 'env notin (dev,testing)', 'team', '!team']

  it.each(expressions)('%s survives parse then stringify', (expr) => {
    expect(stringify(parseSelectorExpression(expr))).toBe(expr)
  })
})

describe('Selector', () => {
  it('matches labels by equality', () => {
    const matches = Selector({matchLabels: {env: 'prod'}})

    expect(matches({env: 'prod'})).toBe(true)
    expect(matches({env: 'dev'})).toBe(false)
    expect(matches({})).toBe(false)
  })

  it('matches set membership expressions', () => {
    const matches = Selector({
      matchExpressions: [{operator: 'NotIn', key: 'env', values: ['dev', 'testing']}],
    })

    expect(matches({env: 'prod'})).toBe(true)
    expect(matches({env: 'dev'})).toBe(false)
  })

  it('requires every expression to match', () => {
    const matches = Selector({
      matchLabels: {env: 'prod'},
      matchExpressions: [{operator: 'Exists', key: 'team', values: []}],
    })

    expect(matches({env: 'prod', team: 'checkout'})).toBe(true)
    expect(matches({env: 'prod'})).toBe(false)
  })

  it('treats missing labels as an empty set', () => {
    const matches = Selector({matchExpressions: [{operator: 'DoesNotExist', key: 'team', values: []}]})

    expect(matches(undefined)).toBe(true)
  })
})
