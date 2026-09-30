import {z} from 'zod'

export const metadata = z.object({
  name: z.string(), uid: z.string(), version: z.number().int(),
  account: z.string().optional(), project: z.string().optional(),
  labels: z.record(z.string(), z.string()).optional(),
  annotations: z.record(z.string(), z.string()).optional(),
  creationTimestamp: z.string().optional(), updateTimestamp: z.string().optional(),
}).loose()

export function manifest<S extends z.ZodType, T extends z.ZodType>(spec: S, status: T) {
  return z.object({apiVersion: z.string().optional(), kind: z.string(), metadata, spec, status: status.optional()}).loose()
}
export function page<T extends z.ZodType>(item: T) {
  return z.object({items: z.array(item), limit: z.number().int().positive(), next: z.string().optional(), total: z.number().optional()})
}
const labels = z.record(z.string(), z.string())
export const selector = z.object({
  matchLabels: labels.optional(),
  matchExpressions: z.array(z.object({key: z.string(), operator: z.string(), values: z.array(z.string()).optional()})).optional(),
}).loose()
export const prob = z.object({kind: z.string(), timeout: z.number().optional(), spec: z.record(z.string(), z.unknown()).optional()}).loose()
export const run = manifest(z.object({probKind: z.string().optional(), start_time: z.string().optional(), end_time: z.string().optional()}).loose(), z.object({
  status: z.enum(['pending', 'running', 'completed', 'timeout', 'errored']).optional(),
  result: z.string().optional(), deadline: z.string().optional(), numberArtifacts: z.number().optional(),
  executor: z.object({runnerId: z.string().optional(), runnerName: z.string().optional(), workerId: z.string().optional(), workerName: z.string().optional()}).loose().optional(),
}).loose())
export const scenario = manifest(z.object({
  description: z.string().optional(), active: z.boolean(), schedule: z.string().optional(), requirements: selector.optional(), prob: prob.optional(),
}).loose(), z.object({nextScheduledRunTime: z.string().optional(), results: z.array(run).optional()}).loose())
export const presence = z.enum(['online', 'offline', 'api-unreachable', 'nats-unreachable', 'unknown'])
export const worker = manifest(z.object({requestedTTL: z.number().optional()}).loose(), z.object({
  paused: z.boolean().optional(), ttl: z.number().optional(),
  lastSeenTime: z.string().nullable().optional(), natsLastSeenTime: z.string().nullable().optional(), leftAt: z.string().nullable().optional(), lastSeenVia: z.string().optional(),
  presence: z.object({condition: presence, api: z.string(), nats: z.string()}).optional(),
}).loose())
export const runner = manifest(z.object({
  active: z.boolean(), description: z.string().optional(), requirements: selector.optional(), maxInstance: z.number().optional(),
}).loose(), z.object({
  numberInstances: z.number().optional(), activeInstances: z.array(z.unknown()).optional(),
  channel: z.object({observed: z.boolean().optional(), pullers: z.number().optional(), pending: z.number().optional()}).loose().optional(),
}).loose())
export const artifact = manifest(z.object({rel: z.string().optional(), mimeType: z.string().optional(), expire_time: z.string().optional(), dataClass: z.string().optional()}).loose(), z.record(z.string(), z.unknown()))
export const placement = z.object({schedulable: z.boolean(), eligibleRunners: z.number(), readyWorkers: z.number(), reason: z.string().optional(), detail: z.string().optional()}).loose()
export type Scenario = z.infer<typeof scenario>
export type Run = z.infer<typeof run>
export type Runner = z.infer<typeof runner>
export type Worker = z.infer<typeof worker>
export const failure = manifest(z.object({reason: z.string(), detail: z.string().optional(), occurredAt: z.string(), resultUID: z.string().optional(), scenarioName: z.string().optional(), runnerUID: z.string().optional(), deliveries: z.number().optional()}).loose(), z.object({resolved: z.boolean(), resolvedAt: z.string().optional(), retryResultUID: z.string().optional(), retryResultName: z.string().optional()}).loose())
