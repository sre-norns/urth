// Placement: why a scenario cannot run, and why a run never did.
//
// The server records both as slugs -- in a placement preview's `reason`, and on
// a Result as the `urth/result.unschedulable` label -- because a slug is
// queryable and a sentence is not. Turning them into sentences is this side's
// job, and it is done here rather than at each use so that the same condition
// reads the same way on the scenario page and on the run it produced.

// Mirrors the reason slugs in pkg/urth/labels.go and the dispatch-failure
// reasons in pkg/urth/dispatch_failure.go, both of which reach a Result through
// the same label.
export const Unschedulable = {
  NoEligibleRunner: 'no-eligible-runner',
  InvalidRequirements: 'invalid-requirements',
  MissingExecutionSnapshot: 'missing-execution-snapshot',
  UndeliverableDispatch: 'undeliverable-dispatch',
  MalformedEnvelope: 'malformed-envelope',
  MisroutedDispatch: 'misrouted-dispatch',
  PolicyRefused: 'policy-refused',
  MaxDeliveryExhausted: 'max-delivery-exhausted',
}

// Why a run never executed. Written in the past tense: by the time this is
// read, the run is terminal history.
const RUN_MESSAGES = {
  [Unschedulable.NoEligibleRunner]: 'No active runner matched this scenario’s requirements, so the run was never dispatched.',
  [Unschedulable.InvalidRequirements]:
    'This scenario’s requirements are not a valid selector, so the run could not be placed on any runner.',
  [Unschedulable.MissingExecutionSnapshot]:
    'This run predates the execution snapshot, so there is no record of what it was created to execute.',
  [Unschedulable.UndeliverableDispatch]: 'The dispatch for this run could never be published to the transport.',
  [Unschedulable.MalformedEnvelope]: 'The dispatch message could not be parsed by the worker that received it.',
  [Unschedulable.MisroutedDispatch]: 'The dispatch was delivered to a runner other than the one it named.',
  [Unschedulable.PolicyRefused]: 'A worker was refused this job permanently, so it was never run.',
  [Unschedulable.MaxDeliveryExhausted]: 'The broker stopped redelivering this dispatch before any worker claimed it.',
}

// unschedulableMessage explains a terminal run that never executed. Unknown
// slugs are shown rather than swallowed: a reason this build does not recognise
// still tells an operator more than silence.
export const unschedulableMessage = (reason) =>
  reason ? RUN_MESSAGES[reason] || `This run was not schedulable: ${reason}.` : null

// unschedulableHint explains a scenario that cannot be run right now, and says
// what would change it. Present tense, and it names the requirement -- "no
// runner matches" is not actionable without knowing what was being matched.
export const unschedulableHint = (preview) => {
  if (!preview || preview.schedulable) {
    return null
  }

  const requirements = preview.requirements || 'no requirements'

  if (preview.reason === Unschedulable.InvalidRequirements) {
    return `This scenario’s requirements are not a valid selector, so it cannot be placed on any runner: ${preview.detail || requirements}`
  }

  if (preview.matchingRunners > 0) {
    const runners = preview.matchingRunners === 1 ? 'runner matches' : 'runners match'

    return `${preview.matchingRunners} ${runners} ${requirements}, but none is active. Enable one, or relax the scenario’s requirements.`
  }

  return `No runner matches ${requirements}. Add a runner with matching labels, or relax the scenario’s requirements.`
}
