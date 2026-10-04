// The identity half of the app through its real routes, session client and
// configuration, against Urth's msw server -- which fails on any request
// without a handler, so each test also pins the routes its screens call.
import {createSessionClient, IdentityProvider} from '@sre-norns/components/identity'
import {QueryClient} from '@tanstack/react-query'
import {render} from '@testing-library/react'
import {http, HttpResponse} from 'msw'
import {createMemoryRouter, RouterProvider} from 'react-router-dom'
import {routes} from '../App'
import {scopedLinks} from './links'
import {server} from '../test/server'
import {identityTerms, runnerRegistration} from './config'
import {sessionConfig} from './session'

export const api = (path: string) => `http://localhost/v1${path}`
export {identityResource as resource} from '../test/identity-fixtures'
import {identityResource as resource} from '../test/identity-fixtures'
export const page = (items: unknown[]) => ({items, limit: 100, total: items.length})

export function signIn(projectIds: string[] = []) {
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
        project_ids: projectIds,
        system_admin: false,
      }),
    ),
    http.get(api('/accounts/acct-1/projects'), () => HttpResponse.json(page([]))),
    http.get(api('/profile'), () =>
      HttpResponse.json(resource('personal-profiles', 'user-1', {displayName: 'Ada'}, {email: 'ada@example.test'})),
    ),
    http.get(api('/profile/accounts'), () =>
      HttpResponse.json(page([{account_id: 'acct-1', name: 'Edge monitoring', role: 'owner', status: 'active'}])),
    ),
    http.get(api('/accounts/acct-1'), () =>
      HttpResponse.json(resource('accounts', 'acct-1', {description: ''}, {}, {name: 'Edge monitoring'})),
    ),
  )
}

export function renderAt(route: string) {
  // A client of Urth's configuration, created after the test stored (or did not
  // store) a session: a client reads storage when it is created.
  const session = createSessionClient(sessionConfig)
  const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
  return render(
    <IdentityProvider
      session={session}
      queryClient={queryClient}
      terms={identityTerms}
      links={scopedLinks(session.accountId)}
      machineRegistration={runnerRegistration}
    >
      <RouterProvider router={createMemoryRouter(routes, {initialEntries: [route]})} />
    </IdentityProvider>,
  )
}
