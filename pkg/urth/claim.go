package urth

import (
	"errors"
	"fmt"
)

// ClaimDisposition classifies why a claim attempt did not succeed, independently
// of any transport. It exists so the retry decision is made once, here, from the
// reason the claim failed -- not re-derived downstream from an HTTP status or, worse,
// a log line.
//
// A delivered dispatch is removed from its queue only when a worker can prove the
// authoritative claim is complete, or that the run no longer needs the message.
// The three failure dispositions keep those cases apart:
//
//   - a momentary inability to decide (ClaimUnavailable) must leave the message
//     for redelivery, because the run may still be pending;
//   - a run that no longer needs the dispatch (ClaimObsolete) must be
//     acknowledged, so it stops being redelivered forever;
//   - a permanent policy refusal (ClaimForbidden) must stop being redelivered to a
//     worker that will never be allowed to run it.
//
// Collapsing these into one status -- as the prototype did, answering every claim
// failure with 401 -- means a transient database blip deletes the only live
// message for a still-pending run.
type ClaimDisposition int

const (
	// ClaimUnavailable: the claim could not be decided right now -- a store,
	// timeout, or internal failure. The run may still be pending, so the dispatch
	// must be retried and never acknowledged. This is also the safe default for
	// any unclassified error.
	ClaimUnavailable ClaimDisposition = iota

	// ClaimObsolete: the run no longer needs this dispatch. It is missing,
	// terminal, superseded by a newer version, or already validly held elsewhere.
	// Redelivery cannot change that, so the dispatch is acknowledged and dropped.
	ClaimObsolete

	// ClaimForbidden: the caller may not run this. Its session is invalid or
	// expired, its worker is paused, or its runner is disabled or mismatched -- a
	// policy decision that redelivery to the same worker will not reverse.
	ClaimForbidden
)

func (d ClaimDisposition) String() string {
	switch d {
	case ClaimUnavailable:
		return "unavailable"
	case ClaimObsolete:
		return "obsolete"
	case ClaimForbidden:
		return "forbidden"
	default:
		return fmt.Sprintf("ClaimDisposition(%d)", int(d))
	}
}

// ClaimError carries a claim failure together with its disposition. The reason is
// for server-side logs only; it is never sent to the worker, which learns only the
// disposition, and only as an HTTP status class. Telling an authenticated worker of
// the correct runner that a run is already taken is an operational signal, not an
// information leak -- but the exact reason still stays on the server.
type ClaimError struct {
	// Disposition is the only part of a claim failure a worker is told.
	Disposition ClaimDisposition

	reason string
	err    error
}

func (e *ClaimError) Error() string {
	switch {
	case e.reason != "" && e.err != nil:
		return fmt.Sprintf("claim %s: %s: %v", e.Disposition, e.reason, e.err)
	case e.err != nil:
		return fmt.Sprintf("claim %s: %v", e.Disposition, e.err)
	default:
		return fmt.Sprintf("claim %s: %s", e.Disposition, e.reason)
	}
}

func (e *ClaimError) Unwrap() error { return e.err }

// claimUnavailable builds a retryable claim failure that wraps the underlying
// cause -- typically a store error -- so it is visible in server logs without ever
// reaching the worker.
func claimUnavailable(reason string, err error) *ClaimError {
	return &ClaimError{Disposition: ClaimUnavailable, reason: reason, err: err}
}

func claimObsolete(reason string) *ClaimError {
	return &ClaimError{Disposition: ClaimObsolete, reason: reason}
}

func claimForbidden(reason string) *ClaimError {
	return &ClaimError{Disposition: ClaimForbidden, reason: reason}
}

// ClaimDispositionOf reports the disposition of a claim error. Any error that is
// not a *ClaimError -- an unexpected panic-recovery, a wrapped store error that
// escaped classification -- is reported as ClaimUnavailable: an unclassified
// failure is retried rather than allowed to strand a possibly-live run by acking
// or terminating its only dispatch. The boolean says whether the error carried an
// explicit disposition.
func ClaimDispositionOf(err error) (ClaimDisposition, bool) {
	var claimErr *ClaimError
	if errors.As(err, &claimErr) {
		return claimErr.Disposition, true
	}
	return ClaimUnavailable, false
}
