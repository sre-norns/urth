import {screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {http, HttpResponse} from 'msw'
import {afterEach, describe, expect, it} from 'vitest'
import {server} from '../test/server'
import {api, page, renderAt, resource, signIn} from '../identity/test-support'

const metadata = {uid: 'worker-1', name: 'edge-worker', version: 3, account: 'acct-1'}
const worker = {apiVersion: 'urth.sre-norns.com/v1', kind: 'workerInstances', metadata, spec: {}, status: {fingerprint: `sha256:${'b'.repeat(64)}`, paused: false, presence: {condition: 'api-unreachable', api: 'offline', nats: 'online'}}}
afterEach(() => sessionStorage.clear())

describe('infrastructure', () => {
  it('shows independent presence and pauses without an edit precondition', async () => {
    signIn()
    let paused = false
    server.use(
      http.get(api('/accounts/acct-1/workers/edge-worker'), () => HttpResponse.json({...worker, status: {...worker.status, paused}})),
      http.put(api('/accounts/acct-1/workers/edge-worker/paused'), async ({request}) => {
        expect(request.headers.get('Authorization')).toBe('Bearer access')
        expect(request.headers.has('If-Match')).toBe(false)
        expect(await request.json()).toEqual({paused: true})
        paused = true
        return HttpResponse.json({...worker, status: {...worker.status, paused}})
      }),
    )
    renderAt('/a/acct-1/workers/edge-worker')
    expect(await screen.findByText('api unreachable')).toBeInTheDocument()
    expect(screen.getByText('offline')).toBeInTheDocument()
    expect(screen.getByText('online')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Pause worker'}))
    expect(await screen.findByRole('button', {name: 'Resume worker'})).toBeInTheDocument()
  })
  it('follows the cursor rather than a total, and resets it on a new filter', async () => {
    signIn()
    const queries: URLSearchParams[] = []
    server.use(http.get(api('/accounts/acct-1/workers'), ({request}) => {
      const params = new URL(request.url).searchParams
      queries.push(params)
      return HttpResponse.json({items: [worker], limit: 20, ...(params.has('cursor') ? {} : {next: 'opaque+/='})})
    }))
    renderAt('/a/acct-1/workers')
    await userEvent.click(await screen.findByRole('button', {name: 'Next'}))
    await waitFor(() => expect(queries.some((q) => q.get('cursor') === 'opaque+/=')).toBe(true))
    await userEvent.type(screen.getByRole('searchbox'), 'edge')
    await userEvent.click(screen.getByRole('button', {name: 'Search'}))
    await waitFor(() => expect(queries.at(-1)?.get('name')).toBe('edge'))
    expect(queries.at(-1)?.has('cursor')).toBe(false)
  })
  it('preserves the draft after a stale runner edit and requires review before retrying', async () => {
    signIn()
    let version = 1
    const writes: string[] = []
    const runner = () => ({apiVersion: 'urth.sre-norns.com/v1', kind: 'runners', metadata: {uid: 'runner-1', name: 'edge', version, account: 'acct-1'}, spec: {active: true, description: version === 1 ? 'Original' : 'Changed elsewhere'}})
    server.use(
      http.get(api('/accounts/acct-1/runners/edge'), () => HttpResponse.json(runner(), {headers: {ETag: `"${version}"`}})),
      http.get(api('/accounts/acct-1/workers'), () => HttpResponse.json(page([]))),
      http.get(api('/agent-identities/runner-1'), () => HttpResponse.json(resource('agent-identities', 'runner-1', {description: 'Identity'}, {}, {name: 'edge'}))),
      http.get(api('/agent-identities/runner-1/tokens'), () => HttpResponse.json(page([]))),
      http.get(api('/agent-identities/runner-1/project-grants'), () => HttpResponse.json(page([]))),
      http.put(api('/accounts/acct-1/runners/edge'), async ({request}) => {
        writes.push(request.headers.get('If-Match')!)
        if (writes.length === 1) {version = 2; return HttpResponse.json({detail: 'Changed elsewhere'}, {status: 412})}
        expect((await request.json() as {spec: {description: string}}).spec.description).toBe('My draft')
        return HttpResponse.json(runner(), {headers: {ETag: '"3"'}})
      }),
    )
    renderAt('/a/acct-1/runners/runner-1')
    await userEvent.click(await screen.findByRole('button', {name: 'Edit scheduling'}))
    const dialog = within(screen.getByRole('dialog'))
    await userEvent.clear(dialog.getByLabelText('Description'))
    await userEvent.type(dialog.getByLabelText('Description'), 'My draft')
    await userEvent.click(dialog.getByRole('button', {name: 'Save'}))
    expect(await dialog.findByRole('heading', {name: 'Resource changed'})).toBeInTheDocument()
    expect(dialog.getByLabelText('Description')).toHaveValue('My draft')
    await userEvent.click(dialog.getByRole('button', {name: 'Load latest resource'}))
    await userEvent.click(await dialog.findByRole('button', {name: 'Apply draft to this version'}))
    await userEvent.click(dialog.getByRole('button', {name: 'Save'}))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(writes).toEqual(['"1"', '"2"'])
  })
  it('blocks and unblocks a fingerprint with version checks and preserves concurrent blocks', async () => {
    signIn()
    let version = 1
    const fingerprint = `sha256:${'a'.repeat(64)}`
    const concurrent = {identity: `sha256:${'c'.repeat(64)}`, reason: 'Separate investigation'}
    let blocks: {identity: string; reason?: string}[] = []
    const writes: string[] = []
    const runner = () => ({apiVersion: 'urth.sre-norns.com/v1', kind: 'runners', metadata: {uid: 'runner-1', name: 'edge', version, account: 'acct-1'}, spec: {active: true, blockedWorkers: blocks}})
    server.use(
      http.get(api('/accounts/acct-1/runners/edge'), () => HttpResponse.json(runner(), {headers: {ETag: `"${version}"`}})),
      http.get(api('/accounts/acct-1/workers'), () => HttpResponse.json(page([]))),
      http.get(api('/agent-identities/runner-1'), () => HttpResponse.json(resource('agent-identities', 'runner-1', {description: 'Identity'}, {}, {name: 'edge'}))),
      http.get(api('/agent-identities/runner-1/tokens'), () => HttpResponse.json(page([]))),
      http.get(api('/agent-identities/runner-1/project-grants'), () => HttpResponse.json(page([]))),
      http.put(api('/accounts/acct-1/runners/edge'), async ({request}) => {
        writes.push(request.headers.get('If-Match')!)
        if (writes.length === 1) {
          version = 2; blocks = [concurrent]
          return HttpResponse.json({detail: 'Changed elsewhere'}, {status: 412})
        }
        blocks = (await request.json() as {spec: {blockedWorkers: typeof blocks}}).spec.blockedWorkers
        version++
        return HttpResponse.json(runner(), {headers: {ETag: `"${version}"`}})
      }),
    )
    renderAt('/a/acct-1/runners/runner-1')
    await userEvent.click(await screen.findByRole('button', {name: 'Block worker'}))
    let dialog = within(screen.getByRole('dialog'))
    await userEvent.type(dialog.getByRole('textbox', {name: /Verified key fingerprint/}), fingerprint)
    await userEvent.type(dialog.getByLabelText('Reason'), 'Retired')
    await userEvent.click(dialog.getByRole('button', {name: 'Block worker'}))
    expect(await dialog.findByRole('heading', {name: 'Resource changed'})).toBeInTheDocument()
    expect(dialog.getByRole('textbox', {name: /Verified key fingerprint/})).toHaveValue(fingerprint)
    expect(dialog.getByLabelText('Reason')).toHaveValue('Retired')
    await userEvent.click(dialog.getByRole('button', {name: 'Load latest resource'}))
    await userEvent.click(await dialog.findByRole('button', {name: 'Apply draft to this version'}))
    await userEvent.click(dialog.getByRole('button', {name: 'Block worker'}))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(blocks).toEqual([concurrent, {identity: fingerprint, reason: 'Retired'}])
    const blockedItem = (await screen.findByText(fingerprint)).closest('li')!
    await userEvent.click(within(blockedItem).getByRole('button', {name: 'Unblock'}))
    dialog = within(screen.getByRole('dialog'))
    expect(dialog.getByText(fingerprint)).toBeInTheDocument()
    await userEvent.click(dialog.getByRole('button', {name: 'Unblock worker'}))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(blocks).toEqual([concurrent])
    expect(writes).toEqual(['"1"', '"2"', '"3"'])
  })
  it('resolves account diagnostic failures without offering a project retry', async () => {
    signIn()
    let resolved = false
    const failure = () => ({apiVersion: 'urth.sre-norns.com/v1', kind: 'dispatch-failures', metadata: {...metadata, name: 'diagnostic-1'}, spec: {reason: 'malformed-envelope', occurredAt: '2026-10-01T00:00:00Z'}, status: {resolved}})
    server.use(
      http.get(api('/accounts/acct-1/dispatch-failures/diagnostic-1'), () => HttpResponse.json(failure())),
      http.post(api('/accounts/acct-1/dispatch-failures/diagnostic-1/resolve'), () => {resolved = true; return HttpResponse.json(failure())}),
    )
    renderAt('/a/acct-1/dead-letters/diagnostic-1')
    await userEvent.click(await screen.findByRole('button', {name: 'Resolve'}))
    expect(screen.queryByRole('button', {name: 'Retry dispatch'})).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Confirm'}))
    expect(await screen.findByText('resolved')).toBeInTheDocument()
  })
})
