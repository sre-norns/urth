import {useEffect, useRef, useState} from 'react'
import {AppShell, Button, ErrorState, LoadingState, MenuLink, NavLabel, ScopeSwitcher} from '@sre-norns/components'
import {
  IdentityUserMenu, isAccountAdmin, PrincipalProvider, useAccountId, useIdentity,
  useIdentityQuery, useIdentityShell, useSessionEpoch, useSessionScope, wire,
} from '@sre-norns/components/identity'
import {Activity, AlertTriangle, FolderKanban, MailPlus, Server, Settings, Users, Cpu, ListChecks, Play, FileBox} from 'lucide-react'
import {Navigate, NavLink, Outlet, useLocation, useNavigate, useParams} from 'react-router-dom'
import {accountPath, projectPath} from './links'

export function Shell() {
  const account = useAccountId()
  const scope = useSessionScope()
  const epoch = useSessionEpoch()
  const location = useLocation()
  const navigate = useNavigate()
  const {accountId, projectId} = useParams()
  const {paths, session, links} = useIdentity()
  const shell = useIdentityShell()
  const [switchError, setSwitchError] = useState<unknown>()
  const [switching, setSwitching] = useState(false)
  const previousAccount = useRef(account)
  const projects = useIdentityQuery(`${paths.accountProjects(account ?? '')}?limit=100`, wire.page(wire.project), {
    enabled: Boolean(shell.principal.data) && (!accountId || account === accountId), allPages: true,
  })
  useEffect(() => {
    if (previousAccount.current && account && previousAccount.current !== account && accountId !== account)
      void navigate(`${accountPath(account)}/projects`, {replace: true})
    previousAccount.current = account
  }, [account, accountId, navigate])

  async function selectAccount(id: string) {
    if (switching || id === account) return
    setSwitchError(undefined)
    setSwitching(true)
    try { await session.switchToAccount(id) }
    catch (error) { setSwitchError(error) }
    finally { setSwitching(false) }
  }

  if (scope === 'system') return <main className="sign-in"><h1>System workspace</h1><p>Urth has no system administration pages yet. Sign in to an account to continue.</p><Button onClick={() => shell.signInElsewhere()}>Sign in to an account</Button></main>
  if (!account) return <Navigate to={`/sign-in?${new URLSearchParams({returnTo: location.pathname + location.search})}`} replace />
  if (shell.principal.isError) return <main className="sign-in"><h1>Session unavailable</h1><ErrorState error={shell.principal.error} retry={() => void shell.principal.refetch()} /><Button onClick={() => shell.signOut()}>Sign out</Button></main>
  if (!shell.principal.data) return <main className="sign-in"><LoadingState /></main>
  if (accountId && accountId !== account) return (
    <main className="sign-in">
      <h1>Different account</h1><p>This address belongs to another account. Switch accounts to open it.</p>
      {Boolean(switchError) && <ErrorState error={switchError} />}
      <Button disabled={switching} onClick={() => void selectAccount(accountId)}>Switch to this account</Button>
      <NavLink to={`${accountPath(account)}/projects`}>Return to current account</NavLink>
    </main>
  )
  const principal = shell.principal.data.data
  const admin = isAccountAdmin(principal)
  const base = accountPath(account)
  const project = projects.data?.data.items.find((p) => p.id === projectId)
  return (
    <PrincipalProvider principal={principal}>
      <AppShell
        brand={{name: 'URTH', tagline: 'Synthetic monitoring', icon: <Activity size={26} />}}
        storageKey="urth.sidebar.collapsed"
        scope={<ScopeSwitcher {...shell.scopes} onSelectAccount={(id) => void selectAccount(id)}
          project={project && {id: project.id, name: project.name}}
          projects={projects.data?.data.items.map((p) => ({id: p.id, name: p.name})) ?? []}
          onSelectProject={(id) => void navigate(`${projectPath(account, id)}/members`)}>
          <MenuLink to={links.projects()}>All projects</MenuLink>
        </ScopeSwitcher>}
        navigation={(close) => <>
          <NavLink to={links.projects()} onClick={close}><FolderKanban size={18} />Projects</NavLink>
          {projectId && <><NavLabel>Project</NavLabel>
            {principal.projectIds.includes(projectId) && <>
            <NavLink to={`${projectPath(account, projectId)}/scenarios`} onClick={close}><ListChecks size={18} />Scenarios</NavLink>
            <NavLink to={`${projectPath(account, projectId)}/runs`} onClick={close}><Play size={18} />Runs</NavLink>
            <NavLink to={`${projectPath(account, projectId)}/artifacts`} onClick={close}><FileBox size={18} />Artifacts</NavLink>
            <NavLink to={`${projectPath(account, projectId)}/dead-letters`} onClick={close}><AlertTriangle size={18} />Project dead letters</NavLink>
            </>}
            <NavLink to={`${projectPath(account, projectId)}/members`} onClick={close}><Users size={18} />Project members</NavLink>
            <NavLink to={`${projectPath(account, projectId)}/runners`} onClick={close}><Server size={18} />Project runners</NavLink>
          </>}
          {admin && <><NavLabel>Account</NavLabel>
            <NavLink to={`${base}/runners`} onClick={close}><Server size={18} />Runners</NavLink>
            <NavLink to={`${base}/workers`} onClick={close}><Cpu size={18} />Workers</NavLink>
            <NavLink to={`${base}/dead-letters`} onClick={close}><AlertTriangle size={18} />Dead letters</NavLink>
            <NavLink to={`${base}/members`} onClick={close}><Users size={18} />Members</NavLink>
            <NavLink to={`${base}/invitations`} onClick={close}><MailPlus size={18} />Invitations</NavLink>
            <NavLink to={`${base}/settings`} onClick={close}><Settings size={18} />Settings</NavLink>
          </>}
        </>}
        topbar={<span className="eyebrow">{project?.name || shell.accountName || account}</span>}
        user={<IdentityUserMenu shell={shell} />}
      >
        {Boolean(switchError || shell.error) && <ErrorState error={switchError || shell.error} />}
        {projects.isError && <ErrorState error={projects.error} retry={() => void projects.refetch()} />}
        <Outlet key={`${account}:${epoch}`} />
      </AppShell>
    </PrincipalProvider>
  )
}
