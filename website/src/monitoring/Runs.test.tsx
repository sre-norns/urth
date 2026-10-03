import {act, screen, waitFor} from '@testing-library/react'
import {http, HttpResponse} from 'msw'
import {afterEach, describe, expect, it, vi} from 'vitest'
import {server} from '../test/server'
import {api, renderAt, resource, signIn} from '../identity/test-support'

const meta = {name: 'run-1', uid: 'run-uid-1', version: 1, account: 'acct-1', project: 'proj-1', labels: {'urth/scenario.name': 'http-check'}}
const run = (status: string, result?: string) => ({apiVersion: 'urth.sre-norns.com/v1', kind: 'results', metadata: meta, spec: {probKind: 'http'}, status: {status, result, numberArtifacts: 2}})
const storedLog = () => new HttpResponse('data: stored line\n\nevent: end\ndata: completed\n\n', {headers: {'Content-Type': 'text/event-stream'}})
// A live tail that stays open, as the server's does while a run executes.
const liveLog = () => new HttpResponse(new ReadableStream({start(c) {c.enqueue(new TextEncoder().encode('data: live line\n\n'))}}), {headers: {'Content-Type': 'text/event-stream'}})
const logs = api('/projects/proj-1/scenarios/http-check/results/run-1/logs')

function project() {
  signIn()
  server.use(
    http.get(api('/principal'), () => HttpResponse.json({type: 'user', user_id: 'user-1', account_id: 'acct-1', credential_id: 'sess-1', account_role: 'owner', project_ids: ['proj-1'], system_admin: false})),
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
