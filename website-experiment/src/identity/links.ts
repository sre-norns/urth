import type {IdentityLinks} from '@sre-norns/components/identity'

export const accountPath = (account: string) => `/a/${encodeURIComponent(account)}`
export const projectPath = (account: string, project: string) => `${accountPath(account)}/p/${encodeURIComponent(project)}`

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
