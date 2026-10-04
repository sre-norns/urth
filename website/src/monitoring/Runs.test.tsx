import {act, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {http, HttpResponse} from 'msw'
import {afterEach, describe, expect, it, vi} from 'vitest'
import {server} from '../test/server'
import {api, page, renderAt, resource, signIn} from '../identity/test-support'

const meta = {name: 'run-1', uid: 'run-uid-1', version: 1, account: 'acct-1', project: 'proj-1', labels: {'urth/scenario.name': 'http-check'}}
const run = (status: string, result?: string) => ({apiVersion: 'urth.sre-norns.com/v1', kind: 'results', metadata: meta, spec: {probKind: 'http'}, status: {status, result, numberArtifacts: 2}})
const storedLog = () => new HttpResponse('data: stored line\n\nevent: end\ndata: completed\n\n', {headers: {'Content-Type': 'text/event-stream'}})
// A live tail that stays open, as the server's does while a run executes.
const liveLog = () => new HttpResponse(new ReadableStream({start(c) {c.enqueue(new TextEncoder().encode('data: live line\n\n'))}}), {headers: {'Content-Type': 'text/event-stream'}})
const logs = api('/projects/proj-1/scenarios/http-check/results/run-1/logs')

function project(member = true) {
  signIn()
  server.use(
    http.get(api('/principal'), () => HttpResponse.json({type: 'user', user_id: 'user-1', account_id: 'acct-1', credential_id: 'sess-1', account_role: 'owner', project_ids: member ? ['proj-1'] : [], system_admin: false})),
    http.get(api('/projects/proj-1'), () => HttpResponse.json(resource('projects', 'proj-1', {description: ''}, {}, {name: 'Plant 2'}))),
  )
}
afterEach(() => {sessionStorage.clear(); vi.useRealTimers()})

describe('run detail', () => {
  it('links the scenario name and the artifact count to their pages', async () => {
    project()
    server.use(http.get(api('/projects/proj-1/results/run-1'), () => HttpResponse.json(run('completed', 'success'))), http.get(logs, storedLog))
    renderAt('/a/acct-1/p/proj-1/runs/run-1')
    const scenarioLinks = await screen.findAllByRole('link', {name: /http-check|Scenario definition/})
    expect(scenarioLinks.map((l) => l.getAttribute('href'))).toEqual(['/a/acct-1/p/proj-1/scenarios/http-check', '/a/acct-1/p/proj-1/scenarios/http-check'])
    const artifacts = '/a/acct-1/p/proj-1/artifacts?labels=urth%2Fresult.uid%3Drun-uid-1'
    expect(screen.getByRole('link', {name: '2'})).toHaveAttribute('href', artifacts)
    expect(screen.getByRole('link', {name: 'Run artifacts'})).toHaveAttribute('href', artifacts)
  })
  it.each(['completed', 'timeout', 'errored'])('offers no log reconnect for a %s run', async (status) => {
    project()
    server.use(http.get(api('/projects/proj-1/results/run-1'), () => HttpResponse.json(run(status))), http.get(logs, storedLog))
    renderAt('/a/acct-1/p/proj-1/runs/run-1')
    await waitFor(() => expect(screen.getByLabelText('Run log')).toHaveTextContent('stored line'))
    expect(screen.queryByRole('button', {name: 'Reconnect log'})).not.toBeInTheDocument()
  })
  it('withdraws reconnect when an open run finishes', async () => {
    vi.useFakeTimers({shouldAdvanceTime: true})
    project()
    let current = run('running')
    server.use(
      http.get(api('/projects/proj-1/results/run-1'), () => HttpResponse.json(current)),
      http.get(logs, () => current.status.status === 'running' ? liveLog() : storedLog()),
    )
    renderAt('/a/acct-1/p/proj-1/runs/run-1')
    await waitFor(() => expect(screen.getByLabelText('Run log')).toHaveTextContent('live line'))
    expect(screen.getByRole('button', {name: 'Reconnect log'})).toBeInTheDocument()
    current = run('completed', 'success')
    await act(async () => {await vi.advanceTimersByTimeAsync(15_000)})
    await waitFor(() => expect(screen.getByLabelText('Run log')).toHaveTextContent('stored line'))
    expect(screen.queryByRole('button', {name: 'Reconnect log'})).not.toBeInTheDocument()
  })
})

describe('run lists', () => {
  it('links each run to its scenario', async () => {
    project()
    server.use(http.get(api('/projects/proj-1/results'), () => HttpResponse.json(page([run('completed', 'success')]))))
    renderAt('/a/acct-1/p/proj-1/runs')
    expect(await screen.findByRole('link', {name: 'http-check'})).toHaveAttribute('href', '/a/acct-1/p/proj-1/scenarios/http-check')
    expect(screen.getByRole('link', {name: 'run-1'})).toHaveAttribute('href', '/a/acct-1/p/proj-1/runs/run-1')
  })
  it('links each artifact to the run that produced it', async () => {
    project()
    const artifact = {apiVersion: 'urth.sre-norns.com/v1', kind: 'artifacts', metadata: {...meta, name: 'trace-1', uid: 'artifact-1', labels: {'urth/result.name': 'run-1'}}, spec: {rel: 'har'}}
    server.use(http.get(api('/projects/proj-1/artifacts'), () => HttpResponse.json(page([artifact]))))
    renderAt('/a/acct-1/p/proj-1/artifacts')
    expect(await screen.findByRole('link', {name: 'run-1'})).toHaveAttribute('href', '/a/acct-1/p/proj-1/runs/run-1')
    expect(screen.getByRole('link', {name: 'trace-1'})).toHaveAttribute('href', '/a/acct-1/p/proj-1/artifacts/trace-1')
  })
})

describe("a project's runner", () => {
  const grants = () => [
    http.get(/\/v1\/agent-identities\/runner-1(\/.*)?$/, () => HttpResponse.json(page([]))),
    http.get(api('/projects/proj-1/agent-authorizations'), () => HttpResponse.json(page([]))),
  ]
  it('pages through the runs placed on it, queued ones included', async () => {
    project()
    const queries: URLSearchParams[] = []
    const queued = {...run('pending'), metadata: {...meta, name: 'run-2', uid: 'run-uid-2'}}
    const claimed = {...run('completed', 'success'), status: {status: 'completed', result: 'success', executor: {runnerId: 'runner-1', workerName: 'edge-a'}}}
    server.use(...grants(), http.get(api('/projects/proj-1/results'), ({request}) => {
      const query = new URL(request.url).searchParams
      queries.push(query)
      return HttpResponse.json(query.get('cursor') ? {items: [claimed], limit: 1} : {items: [queued], limit: 1, next: 'page-2'})
    }))
    renderAt('/a/acct-1/p/proj-1/runners/runner-1')
    const runs = await screen.findByRole('table', {name: 'Runs on this runner'})
    expect(await within(runs).findByRole('link', {name: 'run-2'})).toHaveAttribute('href', '/a/acct-1/p/proj-1/runs/run-2')
    expect(within(runs).getByRole('link', {name: 'http-check'})).toHaveAttribute('href', '/a/acct-1/p/proj-1/scenarios/http-check')
    expect(within(runs).getByText('pending')).toBeInTheDocument()
    expect(within(runs).getByText('Not claimed')).toBeInTheDocument()
    expect(queries[0].get('labels')).toBe('urth/runner.uid=runner-1')
    await userEvent.click(screen.getByRole('button', {name: /next/i}))
    expect(await screen.findByRole('link', {name: 'run-1'})).toBeInTheDocument()
    expect(within(screen.getByRole('table', {name: 'Runs on this runner'})).getByText('edge-a')).toBeInTheDocument()
    expect(queries.at(-1)?.get('cursor')).toBe('page-2')
    expect(queries.at(-1)?.get('labels')).toBe('urth/runner.uid=runner-1')
  })
  it('does not ask for runs a non-member cannot read', async () => {
    project(false)
    let asked = false
    server.use(...grants(), http.get(api('/projects/proj-1/results'), () => {asked = true; return HttpResponse.json(page([]))}))
    renderAt('/a/acct-1/p/proj-1/runners/runner-1')
    expect(await screen.findByText('Join this project to see the runs this runner executed for it.')).toBeInTheDocument()
    expect(asked).toBe(false)
  })
})
