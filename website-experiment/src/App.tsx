import {
  AccountInvitations,
  AccountMembers,
  AccountSettings,
  CreateProject,
  InvitationCancel,
  InvitationContinue,
  MachineIdentities,
  OAuthCallback,
  ProfilePage,
  ProjectsDirectory,
  SessionsPage,
  SignIn,
} from '@sre-norns/components/identity'
import {Navigate, Route, Routes} from 'react-router-dom'
import {NotFound, ProjectPage, ProjectRunnerPage, RunnerPage} from './identity/pages'
import {Shell} from './identity/Shell'

/**
 * Urth's routes. The sign-in routes are the shared identity ones and stand
 * outside the shell: the authorization server returns the browser to
 * /oauth/callback, and an emailed invitation continues at
 * /invitations/continue. The monitoring pages return in M7, on the scoped API;
 * until then LegacyApp keeps them, unmounted.
 */
export function App() {
  return (
    <Routes>
      <Route path="/sign-in" element={<SignIn />} />
      <Route path="/oauth/callback" element={<OAuthCallback />} />
      <Route path="/invitations/continue" element={<InvitationContinue />} />
      <Route path="/invitations/cancel" element={<InvitationCancel />} />
      <Route element={<Shell />}>
        <Route index element={<Navigate to="/projects" replace />} />
        <Route path="projects" element={<ProjectsDirectory />} />
        <Route path="projects/new" element={<CreateProject />} />
        <Route path="projects/:projectId" element={<ProjectPage kind="members" />} />
        <Route path="projects/:projectId/runners" element={<ProjectPage kind="machines" />} />
        <Route path="projects/:projectId/runners/:runnerId" element={<ProjectRunnerPage />} />
        <Route path="profile" element={<ProfilePage />} />
        <Route path="account/sessions" element={<SessionsPage />} />
        <Route path="account/members" element={<AccountMembers />} />
        <Route path="account/invitations" element={<AccountInvitations />} />
        <Route path="account/settings" element={<AccountSettings />} />
        <Route path="account/runners" element={<MachineIdentities />} />
        <Route path="account/runners/:runnerId" element={<RunnerPage />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  )
}
