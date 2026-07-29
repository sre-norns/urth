# 021: Address Runner queues by name and reap orphaned ones

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `blocked` |
| Priority | `P1` |
| Workstream | Runner contract / Durability |
| Depends on | 022 |
| Likely conflicts | 004, 013 (done), 014, 016 |
| Owner | Unclaimed |

## Why This Matters

A Runner's queue is addressed by the Runner's UID, and nothing ever removes it. Those two
facts together mean the ordinary way an operator manages infrastructure-as-code — delete
the resource, re-apply the same manifest — silently strands a queue and leaks a JetStream
consumer, every time.

This was observed rather than reasoned about. While verifying
[task 013](013-bound-and-observe-jetstream.md), a Postgres test run wiped the resource
tables; NATS still held eight durable consumers for Runners that no longer existed, and the
reconciler correctly created a ninth for the re-applied Runner. Consumers are a bounded
JetStream asset with real memory and metadata cost, and nothing in the system will ever read
those eight or the messages behind them.

The recreated Runner is the worse half. It is, to the operator, the same `team-a` Runner
they have always had; to the broker it is a new queue, and everything queued for the
previous generation sits unread until stream `MaxAge` evicts it. Nothing says so anywhere.

[ADR 0007](../../adr/0007-runner-queue-addressing.md) settles this: queues are addressed by
Runner name, so a new generation reattaches to the queue its predecessor left; entitlement
to execute a run stays keyed by UID in Postgres, so a new generation drains its
predecessor's messages without ever being allowed to run them; and queues whose Runner
resource is gone are garbage-collected.

## Evidence

- `pkg/natsq/config.go:54,59`: `JobSubject` and `RunnerConsumerName` derive the subject and
  durable name from the Runner UID.
- `pkg/natsq/config.go:33-35`: `SubjectPrefix` carries a major version explicitly so an
  incompatible layout can coexist. This is the migration mechanism ADR 0007 §6 uses.
- `pkg/natsq/scheduler.go:132`, `pkg/natsq/reconcile.go:19`: the two places a consumer is
  created. There is no third place, and no place one is removed — `DeleteConsumer` appears
  only in tests.
- `pkg/urth/service.go:1364`: `runnersAPIImpl.Delete` is a bare store delete with no
  transport hook.
- `pkg/urth/service.go:656`: placement records `Status.Executor.RunnerID` on the pending
  Result, before commit.
- `pkg/urth/service.go:945`: `ClaimRun` refuses a worker whose Runner UID is not the placed
  one, as `ClaimObsolete`. This is what makes queue inheritance safe.
- `pkg/urth/service.go:1319`: `runnersAPIImpl.update` refuses a name change, so a
  name-addressed queue cannot move under a live Runner.
- `~/workspace/wyrd/pkg/manifest/types.go`: `ValidateSubdomainName` permits `.` and allows
  253 characters. A name used raw would produce a multi-token subject that
  `urth.v1.jobs.*` does not match.
- `pkg/urth/reconcile.go:660`: `reconcileRunnerChannels` iterates `ActiveRunnerUIDs`. The
  reaper is its inverse and must not be built by inverting this listing naively — see the
  Implementation Constraints.

## Required Outcome

- A Runner's job subject and durable consumer name are derived from `metadata.name` through
  one exported encoder in `pkg/natsq`. No call site concatenates a subject.
- The encoder is total over every name `ValidateSubdomainName` accepts, injective, produces
  exactly one NATS subject token, and produces a name valid and in-range for a durable
  consumer. A name is validated before it is encoded.
- The name-keyed layout lives under a new subject major version, disjoint from the existing
  UID-keyed one, so that a Runner *named* like a UUID cannot collide with another Runner's
  UID-keyed subject.
- A Runner deleted and re-applied under the same manifest reattaches to the same queue: its
  Workers bind an existing consumer and its predecessor's messages are delivered to them.
- Those inherited messages are refused at claim and acknowledged, never executed. A run
  placed on a previous generation reaches a terminal state with a reason an operator can
  read; it is not silently re-executed on the new generation.
- Queue assets whose Runner resource does not exist are removed by a control loop, which
  reports what it removed. A **disabled** Runner keeps its queue.
- Migration drains rather than copies: publication moves to the new layout, existing
  consumers are left to drain and are removed when empty, and the old subject is dropped
  from the stream when no consumer remains. No dispatch is republished under a new message
  ID.
- Broker-side counts distinguish inherited-but-unclaimable messages from live backlog, so
  the queue view [task 016](016-runner-queue-operator-visibility.md) builds does not report
  a draining predecessor as a stalled Runner.

## Implementation Constraints

- **A failed or empty Runner listing is never evidence that no Runners exist.** The reaper
  acts only on a listing it knows to be complete; a store error aborts the sweep without
  deleting anything. One unguarded query here deletes the entire fleet's queues. Test this
  explicitly.
- A grace period separates a Runner's deletion from the reaping of its queue, so that a
  delete-then-re-apply performed as two operator commands does not lose the queue between
  them.
- Reaping is destructive and belongs to a control loop under
  [ADR 0006](../../adr/0006-control-loop-placement.md), composed in `pkg/controllers`, not
  to a request path.
- Do not relax the placed-Runner check at `pkg/urth/service.go:945`, and do not compare
  names there. ADR 0007 §3 rests entirely on it.
- Do not rewrite a Result's placement to point at a new generation. A run placed on a
  Runner that no longer exists is a failed run.
- `pkg/urth` must not import `pkg/natsq`. Reaping arrives through a domain-owned interface,
  as `RunnerChannelReconciler` already does.
- Both subject patterns belong to the same stream during migration. A second stream doubles
  the persistence and replication cost ADR 0004 §3 declined to pay, and
  `jobStreamConfig`/`StreamDrift` must agree about the subject list or every start will
  report drift.
- A work-queue stream rejects overlapping consumer filters. The two layouts do not overlap
  by construction; keep it that way, and do not introduce a wildcard consumer.
- Withdrawal by sequence (`DropDispatch`) must keep working across the migration: an entry
  published under the old layout is still withdrawn by its recorded sequence.

## Suggested Implementation Sequence

1. Add the encoder and its property tests — totality over the validated name charset,
   injectivity including the `a.b` versus `a-b` case, token shape, and length bound —
   before anything consumes it.
2. Add the new subject major version alongside the old one in the stream configuration and
   in drift detection.
3. Switch provisioning and publication to the name-keyed layout; leave binding able to find
   an existing consumer under either during migration.
4. Add an integration test for the recreate case: delete a Runner with queued work,
   re-apply the manifest, assert the new generation's Workers receive and acknowledge the
   inherited messages and that the corresponding Results end terminal with a stated reason.
5. Add the reaper as a control loop, with the complete-listing guard and the grace period,
   and its report counters.
6. Add the drain-and-remove path for old-layout consumers and the stream subject cleanup.
7. Update `cmd/api-server/README.md`, the ADR's implementation-status section, and
   `CONTEXT.md`'s Runner-queue vocabulary.

## Non-Goals

- Per-Runner pipeline presentation in the UI and `urthctl`
  ([task 016](016-runner-queue-operator-visibility.md)); this task produces the counts, it
  does not build the views.
- Rechecking execution requirements at claim time
  ([task 022](022-recheck-requirements-at-claim.md)), which is a dependency rather than
  part of this change.
- Runner-scoped NATS credentials ([task 004](004-runner-scoped-nats-credentials.md)), whose
  subject permissions will need the new addressing but whose issuance is separate.
- Capacity-aware placement ([task 014](014-capacity-aware-runner-placement.md)).
- Making Runner names mutable. They are immutable and this task depends on that.

## Acceptance Criteria / Definition of Done

- [ ] Subject and consumer name derive from `metadata.name` through one validated encoder.
- [ ] The encoder is proven total, injective, single-token, and length-bounded by test.
- [ ] A Runner named with a `.` gets a working queue; the old code would have published
      into an uncaptured subject.
- [ ] Delete-and-re-apply reattaches to the same queue; inherited messages are delivered,
      refused at claim, acknowledged, and their Results end terminal with a stated reason.
- [ ] A Runner deleted and not recreated has its queue assets removed after the grace
      period, and the removal is reported.
- [ ] A disabled Runner keeps its queue.
- [ ] A store failure during the sweep deletes nothing.
- [ ] Migration drains without republishing any dispatch under a new message ID.
- [ ] No unrelated changes; `make audit/postgres` passes.

## Required Tests

- Encoder: injectivity over adversarial pairs (`a.b` versus `a-b`), totality across the
  validated charset, output is one token, output length is within the consumer-name bound,
  a 253-character name still yields a usable and distinct identifier.
- A Runner whose name contains a dot receives and executes a dispatch end to end.
- Recreate: queued work on generation A, delete, re-apply, generation B's worker pulls the
  inherited message, the claim is refused as obsolete, the message is acknowledged, and the
  Result is terminal with a reason.
- Reaper: a deleted Runner's consumer is removed after the grace period; a deleted Runner's
  consumer is *not* removed before it; a disabled Runner's consumer is never removed; a
  Runner listing that returns an error removes nothing (assert the consumer still exists).
- Reaper reports counts, and a sweep that removes nothing reports zero rather than failing.
- Migration: a dispatch published under the old layout is still withdrawn by sequence; a
  stream carrying both subject patterns reports no configuration drift.
- Existing `pkg/natsq` drift, limits, and reconcile suites still pass unchanged in meaning.

## Validation

```sh
go test -race -count=1 ./pkg/natsq ./pkg/urth ./pkg/controllers ./cmd/api-server
make audit/postgres
git diff --check
```

Run it, per `CLAUDE.md`: apply a runner, trigger a run, delete the runner while the run is
queued, re-apply the same manifest, and confirm from the worker log and the Result that the
inherited message was drained rather than executed.

## Completion Record

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
