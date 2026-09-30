// Route wrappers around the shared identity screens: they read route
// parameters and supply the page structure the screens expect around them.
import {EmptyState, ErrorState, LoadingState, Tabs} from '@sre-norns/components'
import {
  MachineIdentityDetail,
  ProjectAccess,
  ProjectMachineDetail,
  useIdentity,
  useIdentityQuery,
  wire,
} from '@sre-norns/components/identity'
import type {ReactNode} from 'react'
import {Link, useNavigate, useParams} from 'react-router-dom'

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

/** A project's access: its members and its runners, as two views. */
export function ProjectPage({kind}: {kind: 'members' | 'machines'}) {
  const {projectId = ''} = useParams()
  const {links} = useIdentity()
  const navigate = useNavigate()
  const base = links.project(projectId)
  return (
    <ProjectFrame projectId={projectId}>
      <Tabs
        label="Project access"
        value={kind}
        options={[
          {value: 'members', label: 'Members'},
          {value: 'machines', label: 'Runners'},
        ]}
        onChange={(next) => navigate(next === 'machines' ? `${base}/runners` : base)}
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
  return (
    <EmptyState title="Page not found">
      <Link to="/projects">Return to projects</Link>
    </EmptyState>
  )
}
