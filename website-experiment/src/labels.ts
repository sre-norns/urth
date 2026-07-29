import type {Artifact, DataClass, Labels, Run, Worker} from './types'

const prefix = 'urth/'

export const labels = {
  scenario: {
    name: `${prefix}scenario.name`,
    uid: `${prefix}scenario.uid`,
    kind: `${prefix}scenario.kind`,
  },
  result: {
    name: `${prefix}result.name`,
    state: `${prefix}result.state`,
    result: `${prefix}result.result`,
    unschedulable: `${prefix}result.unschedulable`,
  },
  runner: {
    name: `${prefix}runner.name`,
    uid: `${prefix}runner.uid`,
  },
  worker: {
    name: `${prefix}worker.name`,
    uid: `${prefix}worker.uid`,
    os: `${prefix}worker.os`,
    arch: `${prefix}worker.arch`,
    hostname: `${prefix}worker.hostname`,
    build: `${prefix}worker.build.version`,
  },
  artifact: {
    kind: `${prefix}artifact.kind`,
    mime: `${prefix}artifact.mime`,
    dataClass: `${prefix}artifact.data-class`,
    resultName: `${prefix}result.name`,
  },
} as const

export const isSystemLabel = (key: string) => key.startsWith(prefix)

export function userLabels(value?: Labels): Labels {
  return Object.fromEntries(Object.entries(value ?? {}).filter(([key]) => !isSystemLabel(key)))
}

export function artifactDataClass(artifact?: Artifact): DataClass {
  return (
    artifact?.metadata.labels?.[labels.artifact.dataClass] ||
    artifact?.spec.dataClass ||
    'unknown'
  ) as DataClass
}

export function artifactKind(artifact?: Artifact): string {
  return artifact?.metadata.labels?.[labels.artifact.kind] || artifact?.spec.rel || 'artifact'
}

export function artifactMime(artifact?: Artifact): string {
  return artifact?.metadata.labels?.[labels.artifact.mime] || artifact?.spec.mimeType || 'application/octet-stream'
}

export function runScenarioName(run?: Run): string | undefined {
  return run?.labels?.[labels.scenario.name]
}

export function workerRunnerName(worker?: Worker): string | undefined {
  return worker?.metadata.labels?.[labels.runner.name]
}
