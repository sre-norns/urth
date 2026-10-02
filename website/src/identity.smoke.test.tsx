// Smoke render of the shared identity screens outside Exp-Bench, where they
// were built. It proves the published entry resolves, renders under Urth's own
// React, router and QueryClient, and talks to the /v1 identity routes through
// Urth's msw server, which fails on any request the handlers do not expect.
// The screens get real routes in M7; nothing here is mounted in the app.
import '@sre-norns/components/styles.css'
import '@sre-norns/components/themes/urth.css'
import {
  createSessionClient,
  IdentityProvider,
  PrincipalProvider,
  ProfilePage,
  SessionsPage,
  type IdentityTerms,
  type Principal,
} from '@sre-norns/components/identity'
import {QueryClient} from '@tanstack/react-query'
import {screen, within} from '@testing-library/react'
import {render} from '@testing-library/react'
import {http, HttpResponse} from 'msw'
import type {ReactElement} from 'react'
import {MemoryRouter} from 'react-router-dom'
import {afterEach, describe, expect, it} from 'vitest'
import {server} from './test/server'

const storageKeys = {
  session: 'urth.session',
  authorization: 'urth.authorization',
  accountArchived: 'urth.account-archived',
}

// What a machine identity is called in Urth's UI is an M5 decision; these
// terms only need to differ from the kit's defaults to show they are used.
const terms: Partial<IdentityTerms> = {
  product: 'Urth',
  machine: 'Runner',
  machines: 'Runners',
  clients: {'urth-web': 'Urth web', urthctl: 'urthctl CLI'},
}

const principal: Principal = {
  type: 'user',
  userId: 'user-1',
  accountId: 'acct-1',
  credentialId: 'sess-current',
  accountRole: 'owner',
  projectIds: [],
  systemAdmin: false,
}

import {identityResource as resource} from './test/identity-fixtures'
const page = (items: unknown[]) => ({items, limit: 100, total: items.length})

function renderIdentity(ui: ReactElement) {
  sessionStorage.setItem(
    storageKeys.session,
    JSON.stringify({
      access_token: 'access',
      refresh_token: 'refresh',
      token_type: 'Bearer',
      scope: 'account',
      account_id: principal.accountId,
      expires_in: 900,
      expires_at: Date.now() + 900_000,
    }),
  )
  const session = createSessionClient({clientId: 'urth-web', storageKeys})
  const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
  return render(
    <IdentityProvider session={session} queryClient={queryClient} terms={terms}>
      <PrincipalProvider principal={principal}>
        <MemoryRouter>{ui}</MemoryRouter>
      </PrincipalProvider>
    </IdentityProvider>,
  )
}

afterEach(() => sessionStorage.clear())

describe('shared identity screens in Urth', () => {
  it('renders the profile from the identity API with the bearer token', async () => {
    const authorization: (string | null)[] = []
    server.use(
      http.get('http://localhost/v1/profile', ({request}) => {
        authorization.push(request.headers.get('Authorization'))
        return HttpResponse.json(resource('personal-profiles', 'user-1', {displayName: 'Ada Lovelace'}, {email: 'ada@example.test'}))
      }),
      http.get('http://localhost/v1/accounts/acct-1', () =>
        HttpResponse.json(resource('accounts', 'acct-1', {description: ''}, {}, {name: 'Analytical Engines'})),
      ),
    )

    renderIdentity(<ProfilePage />)

    expect(await screen.findByRole('heading', {name: 'Ada Lovelace'})).toBeInTheDocument()
    expect(screen.getByText('ada@example.test')).toBeInTheDocument()
    expect(await screen.findByText('Analytical Engines')).toBeInTheDocument()
    expect(authorization).toContain('Bearer access')
  })

  it('lists sessions, marks the current one and names clients in Urth terms', async () => {
    const session = (id: string, clientID: string, created: string) =>
      resource('sessions', id, {}, {userId: 'user-1', clientId: clientID, expiresAt: '2026-10-01T00:00:00Z', refreshExpiresAt: '2026-10-30T00:00:00Z'}, {creationTimestamp: created})
    server.use(
      http.get('http://localhost/v1/sessions', () =>
        HttpResponse.json(
          page([
            session('sess-cli', 'urthctl', '2026-09-29T00:00:00Z'),
            session('sess-current', 'urth-web', '2026-09-30T00:00:00Z'),
          ]),
        ),
      ),
    )

    renderIdentity(<SessionsPage />)

    const rows = await screen.findAllByRole('row')
    expect(within(rows[1]).getByText('Current browser session')).toBeInTheDocument()
    expect(rows[1]).toHaveTextContent('Urth web')
    expect(rows[2]).toHaveTextContent('urthctl CLI')
  })
})
