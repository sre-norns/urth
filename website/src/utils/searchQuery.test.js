import {describe, it, expect} from 'vitest'
import {SearchQuery} from './searchQuery.js'
import {Operator, Rule} from './k8s-labels.js'

const params = (init) => new URLSearchParams(init)

describe('SearchQuery parsing', () => {
  it('is empty when constructed without params', () => {
    const query = new SearchQuery()

    expect(query.name).toBeNull()
    expect(query.labels).toBe('')
    expect(query.page).toBeUndefined()
    expect(query.pageSize).toBeUndefined()
  })

  it('reads name, paging and labels from the url', () => {
    const query = new SearchQuery(params('name=checkout&page=2&pageSize=50&labels=env %3D prod'))

    expect(query.name).toBe('checkout')
    expect(query.page).toBe(2)
    expect(query.pageSize).toBe(50)
    expect(query.labels).toBe('env = prod')
  })

  it('exposes labels as parsed rules', () => {
    const query = new SearchQuery(params('labels=env notin (dev,testing)'))

    expect(query.labels).toBe('env notin (dev,testing)')
  })
})

describe('SearchQuery serialisation', () => {
  it('round trips a label selector through url params', () => {
    const query = new SearchQuery(params('labels=env %3D prod'))

    expect(query.urlSearchParams.get('labels')).toBe('env = prod')
  })

  it('drops params that are not set', () => {
    const query = new SearchQuery(params('name=checkout&page=3'))
    query.name = null
    query.page = null

    const urlParams = query.urlSearchParams
    expect(urlParams.has('name')).toBe(false)
    expect(urlParams.has('page')).toBe(false)
  })

  it('renders to a query string', () => {
    const query = new SearchQuery(params('name=checkout'))

    expect(query.toString()).toContain('name=checkout')
  })
})

describe('SearchQuery.setRule', () => {
  it('adds a rule when none is present', () => {
    const query = new SearchQuery()
    query.setRule(new Rule('env', Operator.Equals, ['prod']))

    expect(query.labels).toBe('env = prod')
  })

  it('adds a rule alongside an existing one for a different key', () => {
    const query = new SearchQuery(params('labels=env %3D prod'))
    query.setRule(new Rule('tier', Operator.Equals, ['web']))

    expect(query.labels).toBe('env = prod,tier = web')
  })

  // Clicking a label capsule twice, or clicking a second value for the same
  // key, should narrow the filter rather than produce a selector that can never
  // match: "env = prod,env = dev" matches nothing.
  it('replaces an existing rule for the same key', () => {
    const query = new SearchQuery(params('labels=env %3D prod'))
    query.setRule(new Rule('env', Operator.Equals, ['dev']))

    expect(query.labels).toBe('env = dev')
  })
})
