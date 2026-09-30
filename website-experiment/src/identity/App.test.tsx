import {screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {http, HttpResponse, type JsonBodyType} from 'msw'
import {afterEach, describe, expect, it} from 'vitest'
import {server} from '../test/server'
import {api, resource, page, renderAt, signIn} from './test-support'

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
    expect(await screen.findByRole('link', {name: /Plant 2/})).toHaveAttribute('href', '/a/acct-1/p/proj-1/members')
    expect(screen.getByText('URTH')).toBeInTheDocument()
    expect(screen.getByRole('link', {name: 'Runners'})).toHaveAttribute('href', '/a/acct-1/runners')
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
    server.use(
      http.get(api('/accounts/acct-1/runners'), () => HttpResponse.json(page([]))),
      http.get(api('/accounts/acct-1/runners/edge-eu'), () => HttpResponse.json({apiVersion: 'v1', kind: 'runners', metadata: {uid: 'run-1', name: 'edge-eu', version: 1, account: 'acct-1'}, spec: {active: true, description: 'Frankfurt'}})),
      http.get(api('/accounts/acct-1/workers'), () => HttpResponse.json(page([]))),
      http.get(api('/agent-identities/run-1'), () => HttpResponse.json(resource('run-1', {name: 'edge-eu', description: 'Frankfurt'}))),
      http.get(api('/agent-identities/run-1/project-grants'), () => HttpResponse.json(page([]))),
      http.get(api('/agent-identities/run-1/tokens'), () => HttpResponse.json(page([]))),
      http.post(api('/accounts/acct-1/runners'), async ({request}) => {
        posted.push((await request.json()) as JsonBodyType)
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
    expect(await screen.findByRole('heading', {name: 'edge-eu'})).toBeInTheDocument()
  })

  it('sends a signed-out visitor to sign in', async () => {
    renderAt('/projects')
    expect(await screen.findByRole('heading', {level: 1, name: 'Urth'})).toBeInTheDocument()
  })
  it('does not mount a scoped page for a different session account', async () => {
    signIn()
    renderAt('/a/acct-other/runners')
    expect(await screen.findByRole('heading', {name: 'Different account'})).toBeInTheDocument()
    expect(screen.queryByRole('button', {name: 'Register runner'})).not.toBeInTheDocument()
  })

  it('rejects a project whose returned account disagrees with the route', async () => {
    signIn()
    server.use(http.get(api('/projects/proj-other'), () => HttpResponse.json(resource('proj-other', {name: 'Other project', description: '', account_id: 'acct-other'}))))
    renderAt('/a/acct-1/p/proj-other/members')
    expect(await screen.findByText('This project does not belong to the account in this address.')).toBeInTheDocument()
    expect(screen.queryByRole('heading', {name: 'Other project'})).not.toBeInTheDocument()
  })

})
