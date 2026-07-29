// How a worker's liveness reads on screen.
//
// The server decides *what* is true -- it owns the timeouts and computes
// `status.presence` -- and this module decides only how to say it. Recomputing
// the verdict here from timestamps would put a second definition of "online" in
// the system, and the two would drift.
//
// The states worth distinguishing are not "up" and "down". A worker reaches Urth
// over two independent paths, HTTPS to the API server and NATS to its queue, and
// a worker with one of them broken is a specific, fixable problem rather than a
// worker that is merely absent.

export const WorkerCondition = {
  Online: 'online',
  Offline: 'offline',
  Unknown: 'unknown',
  APIUnreachable: 'api-unreachable',
  NATSUnreachable: 'nats-unreachable',
}

export const WorkerContact = {
  Heartbeat: 'heartbeat',
  Claim: 'claim',
}

const DESCRIPTIONS = {
  [WorkerCondition.Online]: {
    label: 'online',
    color: 'success',
    // Nothing to explain: the worker is doing what it should.
    detail: null,
  },
  [WorkerCondition.Offline]: {
    label: 'offline',
    color: 'error',
    detail: 'not reporting on either path',
  },
  // Half-connected states are warnings, not errors. Something is running and
  // something is wrong with it, and saying which half is the entire value.
  [WorkerCondition.APIUnreachable]: {
    label: 'no API contact',
    color: 'warning',
    detail: 'on its queue but not reaching the API server — it can be offered work and cannot claim it',
  },
  [WorkerCondition.NATSUnreachable]: {
    label: 'not on its queue',
    color: 'warning',
    detail: 'reaching the API server but absent from NATS — it has nowhere to collect work from',
  },
  [WorkerCondition.Unknown]: {
    label: 'presence unknown',
    color: 'neutral',
    detail: 'this worker has never reported its liveness',
  },
}

const UNKNOWN = DESCRIPTIONS[WorkerCondition.Unknown]

// conditionOf reads the server's verdict, defaulting to unknown.
//
// A worker record written before liveness reporting existed carries no presence
// at all, and the honest thing to show for it is "unknown" rather than a colour
// implying we know something.
export const conditionOf = (worker) => worker?.status?.presence?.condition || WorkerCondition.Unknown

// describePresence turns a condition into what to render for it.
export const describePresence = (condition) => DESCRIPTIONS[condition] || UNKNOWN

// isOnline is the narrow question -- both paths working -- used for counting.
export const isOnline = (worker) => conditionOf(worker) === WorkerCondition.Online

// lastSeenAt returns the most recent evidence of life from either path, or null.
//
// The later of the two, because the question a reader is asking is "when did we
// last hear from this thing at all", and answering with the older signal would
// make a half-connected worker look staler than it is.
export const lastSeenAt = (worker) => {
  const status = worker?.status || {}
  const times = [status.lastSeenTime, status.natsLastSeenTime]
    .filter(Boolean)
    .map((value) => new Date(value))
    .filter((date) => !Number.isNaN(date.getTime()))

  if (times.length === 0) {
    return null
  }

  return new Date(Math.max(...times.map((date) => date.getTime())))
}

// contactSuffix names the evidence, when the API path is the fresher of the two.
//
// "last seen 20 seconds ago (claimed a run)" says more than the timestamp alone:
// a heartbeat means the process is up, a claim means it is up and taking work.
export const contactSuffix = (worker) => {
  const status = worker?.status || {}
  if (!status.lastSeenTime || status.lastSeenVia !== WorkerContact.Claim) {
    return ''
  }

  return ' (claimed a run)'
}
