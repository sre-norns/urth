import {screen, waitFor} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {http, HttpResponse} from 'msw'
import {afterEach, describe, expect, it} from 'vitest'
import {server} from '../test/server'
import {api, page, renderAt, resource, signIn} from '../identity/test-support'

const meta = {name: 'http-check', uid: 'scenario-1', version: 1, account: 'acct-1', project: 'proj-1'}
const scenario = {apiVersion: 'v1', kind: 'scenarios', metadata: meta, spec: {active: true, description: 'HTTP check', prob: {kind: 'http', spec: {target: 'https://example.test'}}}}
const run = {apiVersion: 'v1', kind: 'results', metadata: {...meta, name: 'run-1', uid: 'run-1', labels: {'urth/scenario.name': 'http-check'}}, spec: {probKind: 'http'}, status: {status: 'completed', result: 'success'}}
function project(member = true) {
  signIn()
  server.use(
    http.get(api('/principal'), () => HttpResponse.json({type: 'user', user_id: 'user-1', account_id: 'acct-1', credential_id: 'sess-1', account_role: 'owner', project_ids: member ? ['proj-1'] : [], system_admin: false})),
    http.get(api('/projects/proj-1'), () => HttpResponse.json(resource('proj-1', {name: 'Plant 2', description: ''}))),
  )
}
afterEach(() => sessionStorage.clear())

describe('project monitoring', () => {
  it('lists standard scenario manifests', async () => {
    project()
    server.use(http.get(api('/projects/proj-1/scenarios'), () => HttpResponse.json(page([scenario]))))
    renderAt('/a/acct-1/p/proj-1/scenarios')
    expect(await screen.findByRole('link', {name: 'http-check'})).toHaveAttribute('href', '/a/acct-1/p/proj-1/scenarios/http-check')
  })
  it('does not treat account administration as project membership', async () => {
    project(false)
    renderAt('/a/acct-1/p/proj-1/scenarios')
    expect(await screen.findByText('Join this project to view its monitoring resources.')).toBeInTheDocument()
  })
  it('keeps Run now disabled without placement', async () => {
    project()
    server.use(
      http.get(api('/projects/proj-1/scenarios/http-check'), () => HttpResponse.json(scenario, {headers: {ETag: '"1"'}})),
      http.get(api('/projects/proj-1/scenarios/http-check/placement'), () => HttpResponse.json({schedulable: false, eligibleRunners: 0, readyWorkers: 0, reason: 'no-eligible-runner'})),
    )
    renderAt('/a/acct-1/p/proj-1/scenarios/http-check')
    expect(await screen.findByRole('button', {name: 'Run now'})).toBeDisabled()
    expect(await screen.findByText('no-eligible-runner')).toBeInTheDocument()
  })
  it('authenticates scoped stored logs and displays multiline events', async () => {
    project()
    server.use(
      http.get(api('/projects/proj-1/results/run-1'), () => HttpResponse.json(run)),
      http.get(api('/projects/proj-1/scenarios/http-check/results/run-1/logs'), ({request}) => {
        expect(request.headers.get('Authorization')).toBe('Bearer access')
        expect(new URL(request.url).searchParams.has('token')).toBe(false)
        return new HttpResponse('data: first line\ndata: second line\n\nevent: end\ndata: completed\n\n', {headers: {'Content-Type': 'text/event-stream'}})
      }),
    )
    renderAt('/a/acct-1/p/proj-1/runs/run-1')
    await waitFor(() => expect(screen.getByLabelText('Run log')).toHaveTextContent('first line second line'))
  })
  it('does not fetch sensitive artifacts until explicitly revealed', async () => {
    project()
    let reads = 0
    server.use(
      http.get(api('/projects/proj-1/artifacts/trace-1'), () => HttpResponse.json({apiVersion: 'v1', kind: 'artifacts', metadata: {...meta, name: 'trace-1'}, spec: {dataClass: 'secret-bearing', mimeType: 'text/plain'}})),
      http.get(api('/projects/proj-1/artifacts/trace-1/content'), ({request}) => {
        expect(request.headers.get('Authorization')).toBe('Bearer access')
        reads++; return new HttpResponse('Sensitive trace', {headers: {'Content-Type': 'text/plain'}})
      }),
    )
    renderAt('/a/acct-1/p/proj-1/artifacts/trace-1')
    const reveal = await screen.findByRole('button', {name: 'Reveal sensitive content'})
    expect(reads).toBe(0)
    await userEvent.click(reveal)
    expect(await screen.findByText('Sensitive trace')).toBeInTheDocument()
    expect(reads).toBe(1)
  })
  it('creates a manifest scenario using the server probe catalogue', async () => {
    project()
    let body: unknown
    server.use(
      http.get(api('/v1/probs'.replace('/v1', '')), () => HttpResponse.json({data: [{kind: 'http'}]})),
      http.post(api('/projects/proj-1/scenarios'), async ({request}) => {body = await request.json(); return HttpResponse.json(scenario, {status: 201, headers: {ETag: '"1"'}})}),
      http.get(api('/projects/proj-1/scenarios/http-check'), () => HttpResponse.json(scenario)),
      http.get(api('/projects/proj-1/scenarios/http-check/placement'), () => HttpResponse.json({schedulable: true, eligibleRunners: 1, readyWorkers: 1})),
    )
    renderAt('/a/acct-1/p/proj-1/scenarios/new')
    await userEvent.type(await screen.findByLabelText('Name'), 'http-check')
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    expect(await screen.findByRole('heading', {name: 'http-check'})).toBeInTheDocument()
    expect(body).toMatchObject({apiVersion: 'v1', kind: 'scenarios', metadata: {name: 'http-check'}, spec: {active: true, prob: {kind: 'http'}}})
  })
  it('resolves a dead-letter run UID through the project list before reading by name', async () => {
    project()
    const namedRun = {...run, metadata: {...run.metadata, name: 'nightly-42', uid: 'result-uuid', labels: {}}}
    server.use(
      http.get(api('/projects/proj-1/results'), ({request}) => {
        const query = new URL(request.url).searchParams
        expect(query.get('fields')).toBe('metadata.uid=result-uuid')
        expect(query.get('limit')).toBe('1')
        return HttpResponse.json(page([namedRun]))
      }),
      http.get(api('/projects/proj-1/results/nightly-42'), () => HttpResponse.json(namedRun)),
    )
    renderAt('/a/acct-1/p/proj-1/run-ids/result-uuid')
    expect(await screen.findByRole('heading', {name: 'nightly-42'})).toBeInTheDocument()
  })
  it('retries a project dead letter only after confirmation and links the new run', async () => {
    project()
    let retried = false
    const failure = () => ({kind: 'dispatch-failures', metadata: {...meta, name: 'failure-1'}, spec: {reason: 'delivery-exhausted', occurredAt: '2026-10-01T00:00:00Z', resultUID: 'original-uid'}, status: {resolved: retried, ...(retried ? {retryResultUID: 'retry-uid'} : {})}})
    server.use(
      http.get(api('/projects/proj-1/dispatch-failures/failure-1'), () => HttpResponse.json(failure())),
      http.post(api('/projects/proj-1/dispatch-failures/failure-1/retry'), async ({request}) => {
        expect(await request.json()).toEqual({resolve: true})
        retried = true
        return HttpResponse.json({failure: failure(), retry: run})
      }),
    )
    renderAt('/a/acct-1/p/proj-1/dead-letters/failure-1')
    await userEvent.click(await screen.findByRole('button', {name: 'Retry dispatch'}))
    expect(retried).toBe(false)
    await userEvent.click(screen.getByRole('button', {name: 'Confirm'}))
    expect(await screen.findByRole('link', {name: 'Retry run'})).toHaveAttribute('href', '/a/acct-1/p/proj-1/run-ids/retry-uid')
  })

})
