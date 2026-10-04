import type {IdentityLinks} from '@sre-norns/components/identity'

export const accountPath = (account: string) => `/a/${encodeURIComponent(account)}`
export const projectPath = (account: string, project: string) => `${accountPath(account)}/p/${encodeURIComponent(project)}`
export const scenarioPath = (account: string, project: string, scenario: string) => `${projectPath(account, project)}/scenarios/${encodeURIComponent(scenario)}`
export const runPath = (account: string, project: string, run: string) => `${projectPath(account, project)}/runs/${encodeURIComponent(run)}`
export const artifactPath = (account: string, project: string, artifact: string) => `${projectPath(account, project)}/artifacts/${encodeURIComponent(artifact)}`
export const runArtifactsPath = (account: string, project: string, runUID: string) => `${projectPath(account, project)}/artifacts?${new URLSearchParams({labels: `urth/result.uid=${runUID}`})}`

/** Resolve at click time: switching accounts replaces the session, not the provider. */
export function scopedLinks(account: () => string | null): Partial<IdentityLinks> {
  const base = () => account() ? accountPath(account()!) : ''
  return {
    home: () => `${base()}/projects`,
    projects: () => `${base()}/projects`,
    createProject: () => `${base()}/projects/new`,
    project: (id) => `${base()}/p/${encodeURIComponent(id)}/members`,
    projectMachine: (project, machine) => `${base()}/p/${encodeURIComponent(project)}/runners/${encodeURIComponent(machine)}`,
    machineIdentities: () => `${base()}/runners`,
    machineIdentity: (id) => `${base()}/runners/${encodeURIComponent(id)}`,
    accountSettings: () => `${base()}/settings`,
    accountMembers: (search) => `${base()}/members${search ? `?q=${encodeURIComponent(search)}` : ''}`,
    profile: () => '/profile', sessions: () => '/account/sessions',
  }
}
