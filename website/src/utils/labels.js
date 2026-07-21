// Well-known labels applied by the server, mirroring pkg/urth/labels.go. They
// are named here rather than spelled out at each use so that a rename on the
// server is a single change on this side too.
const PREFIX = 'urth/'

export const LabelScenario = {
  Name: `${PREFIX}scenario.name`,
  UID: `${PREFIX}scenario.uid`,
  Version: `${PREFIX}scenario.version`,
  Kind: `${PREFIX}scenario.kind`,
}

export const LabelResult = {
  Name: `${PREFIX}result.name`,
  UID: `${PREFIX}result.uid`,
  Version: `${PREFIX}result.version`,
  State: `${PREFIX}result.state`,
  Result: `${PREFIX}result.result`,
}

export const LabelRunner = {
  Name: `${PREFIX}runner.name`,
  UID: `${PREFIX}runner.uid`,
  Version: `${PREFIX}runner.version`,
}

export const LabelWorker = {
  Name: `${PREFIX}worker.name`,
  UID: `${PREFIX}worker.uid`,
  OS: `${PREFIX}worker.os`,
  Arch: `${PREFIX}worker.arch`,
  BuildVersion: `${PREFIX}worker.build.version`,
}

export const LabelArtifact = {
  Kind: `${PREFIX}artifact.kind`,
  Mime: `${PREFIX}artifact.mime`,
  DataClass: `${PREFIX}artifact.data-class`,
  MayContainSecrets: `${PREFIX}artifact.may-contain-secrets`,
  ResultName: LabelResult.Name,
}

// Data classes declared by the prober that produced an artifact. See
// prob.DataClass on the server: an artifact that declares nothing is `unknown`,
// which counts as unsafe rather than clean.
export const DataClass = {
  Clean: 'clean',
  Redacted: 'redacted',
  SecretBearing: 'secret-bearing',
  Unknown: 'unknown',
}

export const DATA_CLASS_DESCRIPTIONS = {
  [DataClass.Clean]: 'Cannot carry credentials by construction',
  [DataClass.Redacted]: 'Derived from a live exchange, credentials removed',
  [DataClass.SecretBearing]: 'Faithful capture of the exchange; may contain credentials',
  [DataClass.Unknown]: 'The prober made no declaration; treat as sensitive',
}

export const dataClassOf = (artifact) =>
  artifact?.metadata?.labels?.[LabelArtifact.DataClass] || DataClass.Unknown

// Mirrors DataClass.MayContainSecrets on the server: anything that is not
// explicitly clean or redacted is treated as though it carries credentials.
export const mayContainSecrets = (artifact) => {
  const dataClass = dataClassOf(artifact)
  return dataClass !== DataClass.Clean && dataClass !== DataClass.Redacted
}

export const labelOf = (resource, key) => resource?.metadata?.labels?.[key] || resource?.labels?.[key] || null

// Labels under the `urth/` prefix are assigned and owned by the server. They are
// recomputed whenever a resource is saved, so editing one has no lasting effect
// -- which makes offering it for editing worse than not showing it at all.
//
// They are still worth *seeing* on an existing resource, so the UI shows them
// read-only, in the same capsule form the resource lists use.
export const isSystemLabel = (key) => String(key || '').startsWith(PREFIX)

export const splitLabels = (labels) => {
  const user = {}
  const system = {}

  for (const [key, value] of Object.entries(labels || {})) {
    if (isSystemLabel(key)) {
      system[key] = value
    } else {
      user[key] = value
    }
  }

  return { user, system }
}
