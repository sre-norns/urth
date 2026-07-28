# 007: Snapshot Immutable Execution Input on Result

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P0` |
| Workstream | Runner contract / Claim lifecycle |
| Depends on | — |
| Likely conflicts | 002, 006, 008, 011 |
| Owner | `feat/result-execution-snapshot` |

## Why This Matters

A dispatch identifies a Result version, but the Result does not store the probe
definition it was created to execute. Claim authorization reloads the current
Scenario by UID. Editing a Scenario while an older Result waits in a queue changes
what that Result runs without changing its version or audit history.

This violates the promise that a Result is one immutable execution attempt and
that claim authorization returns the snapshot associated with it.

## Evidence

- `pkg/urth/types.go:128-140`: Result spec stores Scenario foreign key and prob
  kind, not the prob definition or Scenario versioned reference.
- `pkg/urth/service.go:581-600`: Result creation loads the then-current Scenario.
- `pkg/urth/service.go:971-983`: claim response reloads the current Scenario.
- `pkg/natsq/envelope.go:45-52`: Result version guards dispatch, while Scenario
  name is explicitly non-authoritative.
- `docs/adr/0004-nats-communication-backbone.md:60-81`: Postgres Result owns the
  immutable execution snapshot returned only after authorization.

## Required Outcome

- Result creation copies the complete executable `prob.Manifest`, effective
  timeout, Scenario UID/name/version, and any typed execution requirements needed
  for claim authorization into an immutable persisted snapshot.
- The snapshot and Result/outbox entry commit atomically once task 002 lands.
- Claim returns only the stored snapshot. Later Scenario edits, disablement, or
  deletion do not change an already scheduled execution attempt.
- Ordinary Result list/get responses do not expose secret-bearing script content;
  the snapshot is disclosed only through an authorized claim or an explicitly
  privileged operator endpoint.
- Legacy pending Results without a snapshot fail closed and are marked with an
  actionable migration reason. They are not silently populated from the latest
  Scenario version.

## Implementation Constraints

- Persist a real value snapshot, not another lazy GORM association.
- Reuse registered prob serialization and preserve the exact typed manifest.
- Snapshot validation happens before persistence; a stored Result must always be
  executable by a compatible Worker or explicitly unschedulable.
- Avoid duplicating mutable Scenario metadata that is irrelevant to execution.
- Version/history displayed in Results must describe the snapshot actually run.
- Consider data classification: probe definitions may contain credentials and
  must not leak through public Result serialization or logs.

## Suggested Implementation Sequence

1. Add a typed execution-snapshot field/value object and serialization tests.
2. Add a regression test: create Result, edit Scenario, claim Result, assert old prob.
3. Populate and validate the snapshot during Result creation.
4. Change `authorizeRun` to use only the Result snapshot.
5. Add safe API serialization and legacy-row migration behavior.
6. Document immutable-at-scheduling semantics.

## Non-Goals

- Scenario revision/history UI beyond showing the version attached to a Result.
- Secret injection at execution time; placeholders may remain in the snapshot.
- Outbox mechanics (task 002) or Runner policy design (task 008).

## Acceptance Criteria / Definition of Done

- [x] Scenario edits cannot change an existing pending Result's executable input.
- [x] Claim succeeds after Scenario deletion using the stored snapshot.
- [x] Result/version/audit metadata identifies the Scenario revision actually run.
- [x] Snapshot content is absent from ordinary list/get serialization and logs.
- [x] Legacy pending rows without snapshots fail closed with an actionable state.
- [x] All registered prob kinds round-trip through snapshot storage.

## Required Tests

- Create Result from probe A, update Scenario to probe B, claim: worker receives A.
- Delete or disable Scenario after scheduling: existing Result still claims A;
  no new Results are scheduled from it.
- Public Result GET/list does not expose A's secret-bearing fields.
- Round-trip HTTP, DNS, TCP, ICMP, gRPC, HAR, REST, and browser manifests.
- Legacy pending Result without snapshot is never executed from current Scenario.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./pkg/natsq ./cmd/api-server ./cmd/nats-worker
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Completion Record

- **Implemented:**
  - `pkg/urth/execution.go`: new `ExecutionSnapshot` value object — scenario
    UID/name/version, placement requirements, and the complete typed
    `prob.Manifest` — with `IsZero` (absent vs empty) and `Validate`. The
    effective timeout is `Prob.Timeout` and is deliberately not duplicated at the
    top level; a second copy is a second thing that can disagree. `Validate` does
    not require the kind to be registered in *this* process, so the API server's
    link-time prober set does not become a scheduling policy.
  - `pkg/urth/types.go`: `ResultSpec.Execution`, stored via
    `gorm:"serializer:json"` and hidden from every serialization
    (`form/json/yaml/xml:"-"`).
  - `pkg/urth/service.go`: `Create` takes and validates the snapshot before
    persisting, and derives the `urth/scenario.*` labels from it rather than from
    the scenario. `authorizeRun` no longer loads the `Scenario` at all — it
    returns the stored snapshot. `ClaimRun` refuses a snapshot-less Result
    *before* committing the claim, so no execution lease is spent on a run that
    cannot be described, and `markUnschedulable` records the refusal.
  - `pkg/urth/labels.go`: `urth/result.unschedulable` and the
    `missing-execution-snapshot` reason slug.
  - `Scheduler.Schedule` lost its `Scenario` parameter, and `ResultLoader`
    lost its `Scenario` return (`pkg/urth/scheduler.go`, `outbox_relay.go`,
    `outbox_store.go`, `pkg/natsq/scheduler.go`, `pkg/redqueue/scheduler.go`).
    The legacy asynq transport marshals the whole prob into its queue message, so
    reading it from a live scenario was the same bug on the dispatch path;
    removing the parameter makes the snapshot the only available source rather
    than a convention each transport has to remember. A snapshot-less Result is a
    permanent dispatch failure, not a retryable one.
  - `NewDispatchOutboxEntry` takes the scenario name from the snapshot rather
    than the lazy `Spec.Scenario` association.
- **Tests added/updated:**
  - `pkg/urth/execution_test.go`: every registered prob kind round-trips through
    the storage encoding as its registered type (not `map[string]any`); the
    script-bearing kinds keep their content; `IsZero` and `Validate`. Asserts
    *stability* rather than identity with the input, because the `http` kind
    embeds prometheus/common's `HTTPClientConfig`, whose zero `ProxyURL`
    normalises on the first encode — a property that predates this change and
    already applies to `Scenario.Spec.Prob`.
  - `pkg/urth/service_execution_test.go` (Postgres-backed): scenario edited after
    scheduling → the worker still receives probe A and the run's
    `urth/scenario.version` still names A's revision; scenario disabled → the
    existing run claims, nothing new schedules; scenario *deleted* → the run
    still claims; JSON and YAML serialization of a Result leak neither the script
    nor the snapshot; a NULLed snapshot fails closed as `errored` +
    `urth/result.unschedulable`, with `ClaimObsolete` so the dispatch is acked;
    the legacy publisher refuses a snapshot-less Result and publishes the
    snapshot, not the edited scenario.
  - `pkg/urth/service_outbox_test.go`: `seedScenario`'s http prob fixture used a
    field the real `http.Spec` does not have. It passed only because no prober
    was linked into the test binary; `execution_test.go` links them all, as the
    API server does, so the fixture is now decoded strictly.
  - Each new assertion was confirmed to fail against the previous behaviour:
    reinstating the scenario reload in `authorizeRun` fails the edit and deletion
    tests; removing `json:"-"` leaks `hunter2` into the Result JSON; removing the
    `ClaimRun` guard leaves the Result `running` instead of `errored`.
- **Documentation updated:** `cmd/api-server/README.md` (new "The execution
  snapshot" section, and the asynq line under transports), `CLAUDE.md` (the
  snapshot invariant, and the strict prob decoding in `pkg/urth` tests).
- **Validation evidence:**
  - `make audit/postgres` — green (vet, staticcheck `-checks=all`, `-race` tests
    including the Postgres-backed ones).
  - Ran the real stack. With a run pending, the scenario's tcp target was changed
    from `127.0.0.1:8080` to `192.0.2.1:8080`; the worker probed
    `target=127.0.0.1`. Setting that run's `execution` column to NULL and
    retrying produced `409 run is not claimable (stale)`, the worker discarded
    the message rather than looping, and the Result became `errored` with
    `urth/result.unschedulable=missing-execution-snapshot`.
- **Follow-ups:**
  - No privileged operator endpoint returns the snapshot. Reading back what a run
    was created to execute is a genuine diagnostic need, but it needs operator
    authentication to exist first — task 005. Recorded rather than guessed at.
  - Claim-time re-evaluation of `Execution.Requirements` is recorded but not
    performed; admission policy is task 008's, and the field is there for it.
  - Unrelated observation from the live run: the blackbox tcp prober prefers IPv6
    and fails to resolve `127.0.0.1`/`localhost` in this environment before
    trying IPv4. Not touched here.
