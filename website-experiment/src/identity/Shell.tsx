import {AppShell, Button, ErrorState, LoadingState, NavLabel} from '@sre-norns/components'
import {
  IdentityUserMenu,
  isAccountAdmin,
  PrincipalProvider,
  useAccountId,
  useIdentityShell,
  useSessionScope,
} from '@sre-norns/components/identity'
import {Activity, FolderKanban, MailPlus, Server, Settings, Users} from 'lucide-react'
import {Navigate, NavLink, Outlet, useLocation} from 'react-router-dom'

/**
 * The signed-in application: an account session, its principal, and the
 * shared shell around the routes. Everything under it may use the identity
 * screens, which read the principal from PrincipalProvider.
 */
export function Shell() {
  const account = useAccountId()
  const scope = useSessionScope()
  const location = useLocation()
  const shell = useIdentityShell()

  if (scope === 'system')
    // Identity can issue a system session, but Urth has no system console yet.
    return (
      <main className="sign-in">
        <h1>System workspace</h1>
        <p>Urth has no system administration pages yet. Sign in to an account to continue.</p>
        <Button onClick={() => shell.signInElsewhere()}>Sign in to an account</Button>
      </main>
    )
  if (!account)
    return (
      <Navigate
        to={`/sign-in?${new URLSearchParams({returnTo: location.pathname + location.search})}`}
        replace
      />
    )
  if (shell.principal.isError)
    return (
      <main className="sign-in">
        <h1>Session unavailable</h1>
        <ErrorState error={shell.principal.error} retry={() => void shell.principal.refetch()} />
        <Button onClick={() => shell.signOut()}>Sign out</Button>
      </main>
    )
  if (!shell.principal.data)
    return (
      <main className="sign-in">
        <LoadingState />
      </main>
    )

  const principal = shell.principal.data.data
  const admin = isAccountAdmin(principal)
  return (
    <PrincipalProvider principal={principal}>
      <AppShell
        brand={{name: 'URTH', tagline: 'Synthetic monitoring', icon: <Activity size={26} />}}
        storageKey="urth.sidebar.collapsed"
        navigation={(close) => (
          <>
            <NavLink to="/projects" onClick={close}>
              <FolderKanban size={18} />
              Projects
            </NavLink>
            {admin && (
              <>
                <NavLabel>Account</NavLabel>
                <NavLink to="/account/runners" onClick={close}>
                  <Server size={18} />
                  Runners
                </NavLink>
                <NavLink to="/account/members" onClick={close}>
                  <Users size={18} />
                  Members
                </NavLink>
                <NavLink to="/account/invitations" onClick={close}>
                  <MailPlus size={18} />
                  Invitations
                </NavLink>
                <NavLink to="/account/settings" onClick={close}>
                  <Settings size={18} />
                  Settings
                </NavLink>
              </>
            )}
          </>
        )}
        topbar={<span className="eyebrow">{shell.accountName || account}</span>}
        user={<IdentityUserMenu shell={shell} />}
      >
        <Outlet />
      </AppShell>
    </PrincipalProvider>
  )
}
