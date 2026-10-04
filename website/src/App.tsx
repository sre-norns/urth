import {ErrorState, LoadingState} from '@sre-norns/components'
import {useAccountId} from '@sre-norns/components/identity'
import type {ComponentType} from 'react'
import {Navigate, useLocation, useParams, useRouteError, type RouteObject} from 'react-router-dom'
import {accountPath, projectPath} from './identity/links'

function identity(name: string) {
  return async () => {
    const screens = await import('@sre-norns/components/identity')
    return {Component: screens[name as keyof typeof screens] as ComponentType}
  }
}
function page(name: 'ProjectsPage' | 'ProjectPage' | 'ProjectRunnerPage' | 'RunnerPage' | 'NotFound') {
  return async () => ({Component: (await import('./identity/pages'))[name]})
}
function LegacyRedirect() {
  const account = useAccountId()
  const {projectId, runnerId} = useParams()
  const location = useLocation()
  if (!account) return <Navigate to={`/sign-in?${new URLSearchParams({returnTo: location.pathname + location.search})}`} replace />
  let target: string
  if (projectId) target = `${projectPath(account, projectId)}/${runnerId ? `runners/${encodeURIComponent(runnerId)}` : location.pathname.endsWith('/runners') ? 'runners' : 'members'}`
  else target = `${accountPath(account)}${location.pathname.replace(/^\/account/, '') === '/' ? '/projects' : location.pathname.replace(/^\/account/, '')}`
  return <Navigate to={target + location.search} replace />
}
function RouteError() {
  return <main className="content"><h1>Page unavailable</h1><ErrorState error={useRouteError()} /></main>
}

/** The browser and tests use the same data router, including lazy route loading. */
export const routes: RouteObject[] = [{
  errorElement: <RouteError />,
  hydrateFallbackElement: <LoadingState />,
  children: [
    {path: '/sign-in', lazy: identity('SignIn')},
    {path: '/oauth/callback', lazy: identity('OAuthCallback')},
    {path: '/invitations/continue', lazy: identity('InvitationContinue')},
    {path: '/invitations/cancel', lazy: identity('InvitationCancel')},
    {path: '/', element: <LegacyRedirect />},
    ...['projects', 'projects/new', 'projects/:projectId', 'projects/:projectId/runners', 'projects/:projectId/runners/:runnerId', 'account/runners', 'account/runners/:runnerId', 'account/members', 'account/invitations', 'account/settings'].map((path) => ({path, element: <LegacyRedirect />})),
    {
      lazy: async () => ({Component: (await import('./identity/Shell')).Shell}),
      children: [
        {path: '/profile', lazy: identity('ProfilePage')},
        {path: '/account/sessions', lazy: identity('SessionsPage')},
        {
          path: '/a/:accountId',
          children: [
            {index: true, element: <Navigate to="projects" replace />},
            {path: 'projects', lazy: page('ProjectsPage')},
            {path: 'projects/new', lazy: identity('CreateProject')},
            {path: 'members', lazy: identity('AccountMembers')},
            {path: 'invitations', lazy: identity('AccountInvitations')},
            {path: 'settings', lazy: identity('AccountSettings')},
            {lazy: async () => ({Component: (await import('./monitoring/common')).AccountOnly}), children: [
              {path: 'runners', lazy: async () => ({Component: (await import('./monitoring/Infrastructure')).Runners})},
              {path: 'runners/:runnerId', lazy: async () => ({Component: (await import('./monitoring/Infrastructure')).RunnerDetail})},
              {path: 'workers', lazy: async () => ({Component: (await import('./monitoring/Infrastructure')).Workers})},
              {path: 'workers/:workerName', lazy: async () => ({Component: (await import('./monitoring/Infrastructure')).WorkerDetail})},
              {path: 'dead-letters', lazy: async () => ({Component: (await import('./monitoring/DeadLetters')).DeadLetters})},
              {path: 'dead-letters/:failureName', lazy: async () => ({Component: (await import('./monitoring/DeadLetters')).DeadLetterDetail})},
            ]},
            {path: 'p/:projectId', lazy: async () => ({Component: (await import('./identity/ProjectScope')).ProjectScope}), children: [
              {index: true, element: <Navigate to="members" replace />},
              {lazy: async () => ({Component: (await import('./monitoring/common')).ProjectMemberOnly}), children: [
                {path: 'scenarios', lazy: async () => ({Component: (await import('./monitoring/Scenarios')).Scenarios})},
                {path: 'scenarios/new', lazy: async () => ({Component: (await import('./monitoring/Scenarios')).ScenarioForm})},
                {path: 'scenarios/:scenarioName', lazy: async () => ({Component: (await import('./monitoring/Scenarios')).ScenarioDetail})},
                {path: 'scenarios/:scenarioName/edit', lazy: async () => ({Component: (await import('./monitoring/Scenarios')).ScenarioForm})},
                {path: 'scenarios/:scenarioName/runs', lazy: async () => ({Component: (await import('./monitoring/Runs')).Runs})},
                {path: 'scenarios/:scenarioName/runs/:runName', lazy: async () => ({Component: (await import('./monitoring/Runs')).RunDetail})},
                {path: 'runs', lazy: async () => ({Component: (await import('./monitoring/Runs')).Runs})},
                {path: 'run-ids/:runId', lazy: async () => ({Component: (await import('./monitoring/Runs')).RunReference})},
                {path: 'runs/:runName', lazy: async () => ({Component: (await import('./monitoring/Runs')).RunDetail})},
                {path: 'artifacts', lazy: async () => ({Component: (await import('./monitoring/Artifacts')).Artifacts})},
                {path: 'artifacts/:artifactName', lazy: async () => ({Component: (await import('./monitoring/Artifacts')).ArtifactDetail})},
                {path: 'dead-letters', lazy: async () => ({Component: (await import('./monitoring/DeadLetters')).DeadLetters})},
                {path: 'dead-letters/:failureName', lazy: async () => ({Component: (await import('./monitoring/DeadLetters')).DeadLetterDetail})},
              ]},
              {path: 'members', lazy: page('ProjectPage')},
              {path: 'runners', lazy: async () => ({Component: (await import('./identity/pages')).ProjectRunnersPage})},
              {path: 'runners/:runnerId', lazy: page('ProjectRunnerPage')},
            ]},
          ],
        },
        {path: '*', lazy: page('NotFound')},
      ],
    },
  ],
}]
