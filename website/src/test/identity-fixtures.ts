// Canonical fixtures match the released identity resource boundary.
export function identityResource(kind: string, uid: string, spec: Record<string, unknown> = {}, status: Record<string, unknown> = {}, metadata: Record<string, unknown> = {}) {
  return {
    apiVersion: 'identity.sre-norns.com/v1', kind,
    metadata: {uid, name: '', version: 1, account: 'acct-1', creationTimestamp: '2026-09-30T01:00:00Z', updateTimestamp: '2026-09-30T01:00:00Z', ...metadata},
    spec, status: {phase: 'active', authority: 'owner', ...status},
  }
}
