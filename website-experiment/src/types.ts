export type Labels = Record<string, string>

export interface Metadata {
  name: string
  uid?: string
  version?: number
  labels?: Labels
  creationTimestamp?: string
  updateTimestamp?: string
}

export interface LabelSelector {
  matchLabels?: Labels
  matchExpressions?: Array<{
    key: string
    operator: string
    values?: string[]
  }>
  // Older responses used this spelling.
  matchSelector?: Array<{
    key: string
    operator: string
    values?: string[]
  }>
}

export interface Manifest<Spec, Status = Record<string, unknown>> {
  kind?: string
  metadata: Metadata
  spec: Spec
  status?: Status
}

export interface Prob {
  kind: string
  timeout?: number
  spec?: Record<string, unknown>
}

export interface ScenarioSpec {
  description?: string
  requirements?: LabelSelector
  schedule?: string
  active: boolean
  prob?: Prob
}

export interface ScenarioStatus {
  nextScheduledRunTime?: string
  results?: Run[]
}

export type Scenario = Manifest<ScenarioSpec, ScenarioStatus>

export type JobStatus = 'pending' | 'running' | 'completed' | 'timeout' | 'errored'
export type RunOutcome = '' | 'success' | 'failed' | 'errored' | 'canceled' | 'timeout'

export interface ExecutorRef {
  runnerId?: string
  runnerName?: string
  workerId?: string
  workerName?: string
}

export interface Run {
  kind?: string
  uid?: string
  name: string
  version?: number
  labels?: Labels
  creationTimestamp?: string
  updateTimestamp?: string
  spec?: {
    probKind?: string
    start_time?: string
    end_time?: string
  }
  status?: {
    status?: JobStatus
    result?: RunOutcome
    executor?: ExecutorRef
    deadline?: string
    numberArtifacts?: number
  }
}

export interface RunnerSpec {
  description?: string
  requirements?: LabelSelector
  active: boolean
  maxInstance?: number
}

export interface ChannelStatus {
  observed?: boolean
  pullers?: number
  pending?: number
}

export interface RunnerStatus {
  numberInstances?: number
  activeInstances?: Worker[]
  channel?: ChannelStatus
}

export type Runner = Manifest<RunnerSpec, RunnerStatus>

export type PresenceCondition =
  | 'online'
  | 'offline'
  | 'unknown'
  | 'api-unreachable'
  | 'nats-unreachable'

export interface WorkerStatus {
  ttl?: number
  paused?: boolean
  lastSeenTime?: string
  lastSeenVia?: 'heartbeat' | 'claim'
  natsLastSeenTime?: string
  leftAt?: string
  presence?: {
    api?: 'online' | 'offline' | 'unknown'
    nats?: 'online' | 'offline' | 'unknown'
    condition?: PresenceCondition
  }
}

export type Worker = Manifest<{requestedTTL?: number}, WorkerStatus>

export interface ArtifactSpec {
  expire_time?: string
  expireTime?: string
  rel?: string
  mimeType?: string
  dataClass?: DataClass | ''
}

export type Artifact = Manifest<ArtifactSpec>
export type DataClass = 'clean' | 'redacted' | 'secret-bearing' | 'unknown'

export interface Placement {
  requirements?: string
  matchingRunners?: number
  eligibleRunners?: number
  registeredWorkers?: number
  readyWorkers?: number
  schedulable?: boolean
  reason?: string
}

export interface ProbKind {
  kind: string
  contentType?: string
}

export interface ListResponse<T> {
  data: T[]
  total: number
}

export interface SearchState {
  name?: string
  labels?: string
  page?: number
  pageSize?: number
  from?: string
  to?: string
}
