import type {
  IdentityTerms,
  MachineIdentity,
  MachineRegistration,
} from '@sre-norns/components/identity'
import {z} from 'zod'
import {jobRequirements, workerRequirements} from '../api/models'

// How Urth names the shared identity concepts. A machine identity is a Runner:
// the machine that runs the probes, as a GitHub runner runs jobs.
export const identityTerms: Partial<IdentityTerms> = {
  product: 'Urth',
  machine: 'Runner',
  machines: 'Runners',
  roles: 'Runner roles',
  grantRoles: [{value: 'runner', label: 'Runner'}],
  clients: {'urth-web': 'Web browser', urthctl: 'urthctl CLI'},
}

// The Runner manifest POST /v1/accounts/:account/runners replies with. Only the
// fields the identity screens show are read.
const runnerReply = z
  .object({
    apiVersion: z.literal('urth.sre-norns.com/v1'),
    kind: z.literal('runners'),
    metadata: z.object({
      uid: z.string().min(1),
      name: z.string(),
      version: z.number().optional(),
      account: z.string().optional(),
    }),
    spec: z.object({description: z.string().optional()}).optional(),
  })
  .transform(
    (runner): MachineIdentity => ({
      id: runner.metadata.uid,
      name: runner.metadata.name,
      // The identity created with the Runner is active; the Runner's own
      // spec.active is set explicitly below.
      status: 'active',
      revision: runner.metadata.version ?? 1,
      accountId: runner.metadata.account,
      createdAt: '',
      updatedAt: '',
      authority: 'account-administrator',
      description: runner.spec?.description ?? '',
    }),
  )

/**
 * A Runner and its machine identity are created together, with one UID, by
 * Urth's runner API; the identity routes refuse to create one alone. The
 * shared "Register runner" dialog therefore posts a Runner manifest here, and
 * everything after -- listing, tokens, suspension, project grants -- uses the
 * identity routes.
 */
export const runnerRegistration: MachineRegistration = {
  path: (accountId) => `/v1/accounts/${encodeURIComponent(accountId)}/runners`,
  body: (values) => ({
    apiVersion: 'urth.sre-norns.com/v1',
    kind: 'runners',
    metadata: {name: values.name},
    // Explicit: spec.active is a plain bool, so an omitted field would create
    // an inactive Runner that placement skips, while its identity looked active.
    spec: {active: true, description: values.description ?? '', jobRequirements: jobRequirements.parse(JSON.parse(values.jobRequirements || '{}')), workerRequirements: workerRequirements.parse(JSON.parse(values.workerRequirements || '{}')), propagatedLabels: z.record(z.string(), z.string()).parse(JSON.parse(values.propagatedLabels || '{}'))},
  }),
  response: runnerReply,
  fields: [
    {name: 'name', label: 'Runner name', required: true},
    {name: 'description', label: 'Description', type: 'textarea'},
    {name: 'jobRequirements', label: 'Job requirements (JSON)', type: 'textarea', hint: 'Explicit probe kinds are required to admit jobs. Duration limits use values such as 30s.'},
    {name: 'workerRequirements', label: 'Worker requirements (JSON)', type: 'textarea', hint: 'Version constraints such as >=1.9.0 <2.0.0, runtimes, platform, and privileges.'},
    {name: 'propagatedLabels', label: 'Propagated labels (JSON)', type: 'textarea', hint: 'Labels copied to each run and its artifacts.'},
  ],
}
