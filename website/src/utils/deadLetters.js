// Helpers for the dead-letter view, kept out of the component so the parts
// worth testing -- the default filter and the flattening of a manifest into a
// row -- can be tested without rendering.

// UNRESOLVED_SELECTOR is the label query behind the default view.
export const UNRESOLVED_SELECTOR = 'urth/dispatch-failure.resolved = false'

// hasResolvedFilter reports whether the caller asked to see resolved failures.
export const hasResolvedFilter = (searchParams) => new URLSearchParams(searchParams).has('all')

// withUnresolvedFilter builds the query sent to the API.
//
// `all` is a view flag rather than something the API understands, so it is
// translated here into the label selector and then removed -- sending it on
// would be an unknown parameter, and leaving the selector off would quietly
// widen the list.
export const withUnresolvedFilter = (searchParams) => {
  const params = new URLSearchParams(searchParams)
  const showAll = params.has('all')
  params.delete('all')

  if (showAll) {
    return params.toString()
  }

  const existing = params.get('labels')
  params.set('labels', existing ? `${existing},${UNRESOLVED_SELECTOR}` : UNRESOLVED_SELECTOR)

  return params.toString()
}

// deadLetterRows flattens the API's manifests into what the table draws.
//
// A dispatch failure is a normal resource -- name and labels under `metadata`,
// unlike a run result, which is flat -- so this reads from both `metadata` and
// `spec`/`status` rather than the top level.
export const deadLetterRows = (response) => {
  if (!response || !Array.isArray(response.data)) {
    return []
  }

  return response.data.map((entry) => {
    const metadata = entry.metadata || {}
    const spec = entry.spec || {}
    const status = entry.status || {}
    const labels = metadata.labels || {}

    return {
      name: metadata.name,
      reason: spec.reason || 'unknown',
      scenarioName: spec.scenarioName || '',
      runnerName: labels['urth/runner.name'] || '',
      detail: spec.detail || '',
      deliveries: spec.deliveries || 0,
      reportedBy: spec.reportedBy || '',
      resolved: Boolean(status.resolved),
      retryResultName: status.retryResultName || '',
    }
  })
}
