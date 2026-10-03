// Route wrappers around the shared identity screens: they read route
// parameters and supply the page structure the screens expect around them.
import {Card, EmptyState, ErrorState, LoadingState, Status, Tabs} from '@sre-norns/components'
import {
  MachineIdentityDetail,
  ProjectAccess,
  ProjectMachineDetail,
  ProjectsDirectory,
  useAccountId,
  useIdentity,
  useIdentityQuery,
  usePrincipal,
  wire,
  type Project,
} from '@sre-norns/components/identity'
import type {ReactNode} from 'react'
import {Link, useNavigate, useParams} from 'react-router-dom'
import {z} from 'zod'
import {page} from '../api/models'
import {resourcePath, useResource} from '../api/queries'
import {projectPath} from './links'

/**
 * The project page around the shared project screens. ProjectAccess and
 * ProjectMachineDetail have no h1 of their own -- they expect a product's
 * project page around them -- so this supplies it: the project's name.
 */
function ProjectFrame({projectId, children}: {projectId: string; children: ReactNode}) {
  const {paths} = useIdentity()
  const project = useIdentityQuery(paths.project(projectId), wire.project)
  if (project.isError) return <ErrorState error={project.error} retry={() => void project.refetch()} />
  if (!project.data) return <LoadingState />
  return (
    <>
      <header className="page-heading">
        <div>
          <span className="eyebrow">Project</span>
          <h1>{project.data.data.name}</h1>
          {project.data.data.description && <p>{project.data.data.description}</p>}
        </div>
      </header>
      {children}
    </>
  )
}

/**
 * The account's projects, each opening on its scenarios -- what a project is
 * for in Urth -- rather than on its access, which stays one link away.
 */
export function ProjectsPage() {
  return (
    <ProjectsDirectory
      renderCard={(project) => <ProjectCard project={project} />}
      table={{headings: ['Project', 'Scenarios', 'Status', 'Last update'], renderRow: (project) => <ProjectRow project={project} />}}
    />
  )
}

/**
 * Monitoring needs project membership, account administrators included, so a
 * non-member opens the project's access instead of a refusal.
 */
function useProjectHome(project: Project) {
  const account = useAccountId() ?? ''
  const member = usePrincipal().projectIds.includes(project.id)
  const base = projectPath(account, project.id)
  return {member, home: `${base}/${member ? 'scenarios' : 'members'}`, access: `${base}/members`}
}

/**
 * The count is the scenario list's advisory total, read from a one-item page:
 * the same project-scoped route the Scenarios page uses, so membership is
 * enforced by the server and nothing is asked of a project the caller cannot read.
 */
function ScenarioCount({projectId, member}: {projectId: string; member: boolean}) {
  const scenarios = useResource(`${resourcePath('projects', projectId, 'scenarios')}?limit=1`, page(z.unknown()), projectId, {enabled: member})
  if (!member) return <span className="muted">Not a member</span>
  const total = scenarios.data?.data.total
  if (scenarios.isPending) return <span className="muted">Counting scenarios</span>
  if (total === undefined) return <span className="muted">Scenarios unavailable</span>
  return <span>{total === 1 ? '1 scenario' : `${total} scenarios`}</span>
}

function ProjectCard({project}: {project: Project}) {
  const {member, home, access} = useProjectHome(project)
  return (
    <Card className="project-card">
      <div className="card-meta">
        <code>{project.id.slice(0, 8)}</code>
        <Status value={project.status} />
      </div>
      <h2>
        <Link to={home}>{project.name}</Link>
      </h2>
      <p className="muted">{project.description || 'No description'}</p>
      <p><ScenarioCount projectId={project.id} member={member} /></p>
      <div className="card-footer">
        <small>Updated {new Date(project.updatedAt).toLocaleDateString()}</small>
        {member && <Link to={access} aria-label={`${project.name} members`}>Members</Link>}
      </div>
    </Card>
  )
}

function ProjectRow({project}: {project: Project}) {
  const {member, home} = useProjectHome(project)
  return (
    <tr>
      <td>
        <Link to={home}>{project.name}</Link>
        <p>
          <small>{project.description}</small>
        </p>
      </td>
      <td><ScenarioCount projectId={project.id} member={member} /></td>
      <td>
        <Status value={project.status} />
      </td>
      <td>{new Date(project.updatedAt).toLocaleDateString()}</td>
    </tr>
  )
}

export function ProjectRunnersPage() {
  return <ProjectPage kind="machines" />
}

/** A project's access: its members and its runners, as two views. */
export function ProjectPage({kind = 'members'}: {kind?: 'members' | 'machines'}) {
  const {projectId = ''} = useParams()
  const {links} = useIdentity()
  const navigate = useNavigate()
  const base = links.project(projectId).replace(/\/members$/, '')
  return (
    <ProjectFrame projectId={projectId}>
      <Tabs
        label="Project access"
        value={kind}
        options={[
          {value: 'members', label: 'Members'},
          {value: 'machines', label: 'Runners'},
        ]}
        onChange={(next) => navigate(next === 'machines' ? `${base}/runners` : `${base}/members`)}
      />
      <ProjectAccess projectId={projectId} kind={kind} key={kind} />
    </ProjectFrame>
  )
}

/** A runner's grant within one project. */
export function ProjectRunnerPage() {
  const {projectId = '', runnerId = ''} = useParams()
  return (
    <ProjectFrame projectId={projectId}>
      <ProjectMachineDetail projectId={projectId} machineId={runnerId} />
    </ProjectFrame>
  )
}

/** A runner of the account: its identity, tokens and project grants. */
export function RunnerPage() {
  const {runnerId = ''} = useParams()
  return <MachineIdentityDetail id={runnerId} />
}

export function NotFound() {
  const {links} = useIdentity()
  return (
    <EmptyState title="Page not found">
      <Link to={links.projects()}>Return to projects</Link>
    </EmptyState>
  )
}
