import {createSessionClient} from '@sre-norns/components/identity'
import {http, HttpResponse} from 'msw'
import {afterEach, describe, expect, it} from 'vitest'
import {z} from 'zod'
import {server} from '../test/server'
import {sessionConfig} from '../identity/session'
import {ApiProblem, request} from './http'
import {listPath, resourcePath} from './queries'
import {page, run} from './models'

function signedIn() {
  sessionStorage.setItem('urth.session', JSON.stringify({access_token: 'access', refresh_token: 'refresh', token_type: 'Bearer', scope: 'account', account_id: 'acct-1', expires_in: 900, expires_at: Date.now() + 900_000}))
  return createSessionClient(sessionConfig)
}
afterEach(() => sessionStorage.clear())
const path = '/v1/projects/proj-1/scenarios/check'
const schema = z.object({description: z.string()})

describe('scoped product requests', () => {
  it('carries the read ETag verbatim to PUT, authenticates both requests and keeps the returned ETag', async () => {
    const session = signedIn()
    server.use(
      http.get(`http://localhost${path}`, ({request}) => {
        expect(request.headers.get('Authorization')).toBe('Bearer access')
        return HttpResponse.json({description: 'original'}, {headers: {ETag: '"7"'}})
      }),
      http.put(`http://localhost${path}`, async ({request}) => {
        expect(request.headers.get('Authorization')).toBe('Bearer access')
        expect(request.headers.get('If-Match')).toBe('"7"')
        expect(await request.json()).toEqual({description: 'edited'})
        return HttpResponse.json({description: 'edited'}, {headers: {ETag: '"8"'}})
      }),
    )
    const loaded = await request(session, path, schema)
    const saved = await request(session, path, schema, {method: 'PUT', body: {description: 'edited'}, etag: loaded.etag!})
    expect(saved.etag).toBe('"8"')
  })
  it.each([409, 412, 428])('preserves conflict %s without silently retrying a write', async (status) => {
    const session = signedIn()
    let writes = 0
    server.use(http.put(`http://localhost${path}`, () => {
      writes++
      return HttpResponse.json({title: 'Concurrent update', detail: 'Reload before saving.'}, {status, headers: {'Content-Type': 'application/problem+json'}})
    }))
    const failure = await request(session, path, schema, {method: 'PUT', body: {description: 'draft'}, etag: '"1"'}).catch((e: unknown) => e)
    expect(failure).toBeInstanceOf(ApiProblem)
    expect(failure).toMatchObject({status, conflict: true, message: 'Reload before saving.'})
    expect(writes).toBe(1)
  })
  it.each(['', '*'])('refuses unsafe edit ETag %j before sending a request', async (etag) => {
    await expect(request(signedIn(), path, schema, {method: 'PUT', body: {}, etag})).rejects.toThrow('Load the resource')
  })
  it('keeps an opaque cursor and uses the shared list contract', () => {
    const path = resourcePath('projects', 'project/a', 'results')
    const url = new URL(listPath(path, {cursor: 'opaque+/=', labels: 'env = prod', from: '2026-10-01T00:00:00.000Z'}), 'http://localhost')
    expect(url.pathname).toBe('/v1/projects/project%2Fa/results')
    expect(url.searchParams.get('cursor')).toBe('opaque+/=')
    expect(url.searchParams.get('limit')).toBe('20')
    expect(url.searchParams.get('from')).toBe('2026-10-01T00:00:00Z')
    expect(url.searchParams.has('offset')).toBe(false)
  })
  it('requires manifest results and cursor pages; total is optional', () => {
    const result = {apiVersion: 'urth.sre-norns.com/v1', kind: 'results', metadata: {uid: 'run-1', name: 'check-1', version: 1}, spec: {}, status: {status: 'pending'}}
    expect(page(run).parse({items: [result], limit: 20, next: 'opaque'}).next).toBe('opaque')
    expect(run.safeParse({name: 'old-flat-run', spec: {}, status: {status: 'pending'}}).success).toBe(false)
  })
})

it('rejects unrelated resource groups and kinds', () => {
  const canonical = {apiVersion: 'urth.sre-norns.com/v1', kind: 'results', metadata: {uid: 'r', name: 'run', version: 1}, spec: {}}
  expect(run.safeParse(canonical).success).toBe(true)
  expect(run.safeParse({...canonical, apiVersion: 'v1'}).success).toBe(false)
  expect(run.safeParse({...canonical, apiVersion: 'identity.sre-norns.com/v1'}).success).toBe(false)
  expect(run.safeParse({...canonical, kind: 'scenarios'}).success).toBe(false)
})
