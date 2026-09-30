// The identity half of the app through its real routes, session client and
// configuration, against Urth's msw server -- which fails on any request
// without a handler, so each test also pins the routes its screens call.
import {createSessionClient, IdentityProvider} from '@sre-norns/components/identity'
import {QueryClient} from '@tanstack/react-query'
import {render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {http, HttpResponse, type JsonBodyType} from 'msw'
import {MemoryRouter} from 'react-router-dom'
import {afterEach, describe, expect, it} from 'vitest'
import {App} from '../App'
import {server} from '../test/server'
import {identityLinks, identityTerms, runnerRegistration} from './config'
import {sessionConfig} from './session'

const api = (path: string) => `http://localhost/v1${path}`
const stamp = '2026-09-30T01:00:00Z'
const resource = (id: string, extra: Record<string, unknown> = {}) => ({
  id,
  name: '',
  status: 'active',
  revision: 1,
  account_id: 'acct-1',
  created_at: stamp,
  updated_at: stamp,
  authority: 'owner',
  ...extra,
})
const page = (items: unknown[]) => ({items, limit: 100, total: items.length})

function signIn() {
  sessionStorage.setItem(
    'urth.session',
    JSON.stringify({
      access_token: 'access',
      refresh_token: 'refresh',
      token_type: 'Bearer',
      scope: 'account',
      account_id: 'acct-1',
      expires_in: 900,
      expires_at: Date.now() + 900_000,
    }),
  )
  // What every signed-in page reads: the shell's principal, profile and account.
  server.use(
    http.get(api('/principal'), () =>
      HttpResponse.json({
        type: 'user',
        user_id: 'user-1',
        account_id: 'acct-1',
        credential_id: 'sess-1',
        account_role: 'owner',
        project_ids: [],
        system_admin: false,
      }),
    ),
    http.get(api('/profile'), () =>
      HttpResponse.json({user_id: 'user-1', email: 'ada@example.test', display_name: 'Ada', status: 'active', revision: 1}),
    ),
    http.get(api('/profile/accounts'), () =>
      HttpResponse.json(page([{account_id: 'acct-1', name: 'Edge monitoring', role: 'owner', status: 'active'}])),
    ),
    http.get(api('/accounts/acct-1'), () =>
      HttpResponse.json(resource('acct-1', {name: 'Edge monitoring', description: ''})),
    ),
  )
}

function renderAt(route: string) {
  // A client of Urth's configuration, created after the test stored (or did not
  // store) a session: a client reads storage when it is created.
  const session = createSessionClient(sessionConfig)
  const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
  return render(
    <IdentityProvider
      session={session}
      queryClient={queryClient}
      terms={identityTerms}
      links={identityLinks}
      machineRegistration={runnerRegistration}
    >
      <MemoryRouter initialEntries={[route]}>
        <App />
      </MemoryRouter>
    </IdentityProvider>,
  )
}

afterEach(() => sessionStorage.clear())

describe('Urth identity routes', () => {
  it("lists the account's projects in Urth's shell", async () => {
    signIn()
    server.use(
      http.get(api('/accounts/acct-1/projects'), () =>
        HttpResponse.json(page([resource('proj-1', {name: 'Plant 2', description: 'Floor network'})])),
      ),
    )
    renderAt('/projects')
    expect(await screen.findByRole('link', {name: /Plant 2/})).toHaveAttribute('href', '/projects/proj-1')
    expect(screen.getByText('URTH')).toBeInTheDocument()
    expect(screen.getByRole('link', {name: 'Runners'})).toHaveAttribute('href', '/account/runners')
  })

  it("heads a project's access with the project's name", async () => {
    signIn()
    server.use(
      http.get(api('/projects/proj-1'), () =>
        HttpResponse.json(resource('proj-1', {name: 'Plant 2', description: 'Floor network'})),
      ),
      http.get(api('/projects/proj-1/memberships'), () => HttpResponse.json(page([]))),
    )
    renderAt('/projects/proj-1')
    expect(await screen.findByRole('heading', {level: 1, name: 'Plant 2'})).toBeInTheDocument()
    expect(screen.getByRole('tab', {name: 'Runners'})).toBeInTheDocument()
  })

  it("heads a project's runner with the project's name", async () => {
    // ProjectMachineDetail has no h1 of its own; axe flagged the page without one.
    signIn()
    server.use(
      http.get(api('/projects/proj-1'), () => HttpResponse.json(resource('proj-1', {name: 'Plant 2', description: ''}))),
    )
    renderAt('/projects/proj-1/runners/run-1')
    expect(await screen.findByRole('heading', {level: 1, name: 'Plant 2'})).toBeInTheDocument()
  })

  it("registers a runner through Urth's runner API", async () => {
    signIn()
    const posted: JsonBodyType[] = []
    let registered = false
    server.use(
      http.get(api('/accounts/acct-1/agent-identities'), () =>
        HttpResponse.json(page(registered ? [resource('run-1', {name: 'edge-eu', description: 'Frankfurt'})] : [])),
      ),
      http.get(api('/agent-identities/run-1/tokens'), () => HttpResponse.json(page([]))),
      http.post(api('/accounts/acct-1/runners'), async ({request}) => {
        posted.push((await request.json()) as JsonBodyType)
        registered = true
        return HttpResponse.json(
          {
            apiVersion: 'v1',
            kind: 'runners',
            metadata: {uid: 'run-1', name: 'edge-eu', version: 1, account: 'acct-1'},
            spec: {active: true, description: 'Frankfurt'},
          },
          {status: 201},
        )
      }),
    )
    const user = userEvent.setup()
    renderAt('/account/runners')
    await user.click(await screen.findByRole('button', {name: 'Register runner'}))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText(/Runner name/), 'edge-eu')
    await user.type(within(dialog).getByLabelText(/Description/), 'Frankfurt')
    await user.click(within(dialog).getByRole('button', {name: 'Save'}))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(posted).toEqual([
      {apiVersion: 'v1', kind: 'runners', metadata: {name: 'edge-eu'}, spec: {active: true, description: 'Frankfurt'}},
    ])
    expect(await screen.findByRole('link', {name: 'edge-eu'})).toHaveAttribute('href', '/account/runners/run-1')
  })

  it('sends a signed-out visitor to sign in', async () => {
    renderAt('/projects')
    expect(await screen.findByRole('heading', {level: 1, name: 'Urth'})).toBeInTheDocument()
  })
})
