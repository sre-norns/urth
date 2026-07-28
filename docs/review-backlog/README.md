# NATS Runner Review Backlog

This directory turns the NATS Worker implementation review into agent-ready
tasks. It is deliberately more explicit than `TODO.md`: each task records impact,
source evidence, constraints, ordering, tests, and a Definition of Done.

Read [`CONTEXT.md`](CONTEXT.md) before claiming a task. Accepted ADRs remain the
architectural authority; this backlog describes the work needed to make the
current implementation satisfy them.

Tasks 001–016 came out of that review. Later numbers may be work found while
executing it, which belongs in the same tracker and same format even where it
sits outside the NATS workstreams; such a task says so in its Workstream field.

## Agent Workflow

1. Choose a `ready` task whose dependencies are `done`.
2. Read its entire file, the shared context, and linked ADRs.
3. Revalidate source evidence against the current branch.
4. Check `git status --short` and coordinate around likely conflicts.
5. Mark the task `in-progress` and record an owner/branch before editing.
6. Keep work inside the task's Required Outcome and Non-Goals.
7. Add the required regression/failure tests and run validation.
8. Fill the completion record, mark the task `done`, and update this index.

Status values:

- `ready`: may be claimed now.
- `blocked`: waiting on listed dependencies or a recorded decision.
- `in-progress`: claimed; inspect owner and conflicts before editing.
- `done`: acceptance criteria and completion record are satisfied.

## Task Index

| ID | Priority | Status | Task | Depends on | Likely conflicts |
|---|---|---|---|---|---|
| 001 | P0 | done | [Preserve retryable claim failures](tasks/001-preserve-retryable-claim-failures.md) | — | 003, 010, 011 |
| 002 | P0 | done | [Add the transactional dispatch outbox](tasks/002-transactional-dispatch-outbox.md) | — | 003, 007, 011, 012 |
| 003 | P0 | done | [Reconcile dispatch and execution lifecycle](tasks/003-reconcile-dispatch-and-execution.md) | 002 | 001, 011, 012 |
| 004 | P0 | ready | [Issue Runner-scoped NATS credentials](tasks/004-runner-scoped-nats-credentials.md) | — | 005, 009, 011, 013 |
| 005 | P0 | ready | [Secure Runner enrollment issuance and rotation](tasks/005-secure-runner-enrollment.md) | — | 004, 006, 009 |
| 006 | P0 | ready | [Harden run capabilities and reporting authorization](tasks/006-harden-run-capabilities.md) | — | 005, 007, 008, 011 |
| 007 | P0 | done | [Snapshot immutable execution input on Result](tasks/007-snapshot-result-execution-input.md) | — | 002, 006, 008, 011 |
| 008 | P0 | ready | [Complete the Runner channel policy contract](tasks/008-runner-channel-policy.md) | — | 006, 007, 009, 014 |
| 009 | P0 | blocked | [Add stable Worker identity and Runner blocklists](tasks/009-worker-identity-and-blocklist.md) | 005 | 004, 008 |
| 010 | P1 | ready | [Synchronously acknowledge claimed dispatches](tasks/010-synchronous-jetstream-ack.md) | — | 001, 003, 011 |
| 011 | P1 | blocked | [Exercise the NATS Worker end to end and at crash points](tasks/011-nats-worker-failure-integration-tests.md) | 001–010 | all runtime tasks |
| 012 | P1 | done | [Implement an operational dead-letter workflow](tasks/012-dead-letter-workflow.md) | 003 (done) | 002, 003, 013 |
| 013 | P1 | done | [Bound and observe JetStream assets](tasks/013-bound-and-observe-jetstream.md) | — | 004, 012 |
| 014 | P2 | ready | [Make Runner placement capacity-aware](tasks/014-capacity-aware-runner-placement.md) | — | 008 |
| 015 | P1 | blocked | [Drain Asynq and retire the legacy job model](tasks/015-retire-asynq-transport.md) | 001–013 | all runtime tasks |
| 016 | P1 | ready | [Surface Runner queue state to operators](tasks/016-runner-queue-operator-visibility.md) | 012 (done), 013 (done) | 012, 013, 014 |
| 017 | P1 | ready | [Apply prober config defaults on every authoring path](tasks/017-apply-prober-config-defaults.md) | — | 015 |
| 018 | P1 | done | [Fail unplaceable runs instead of queueing them forever](tasks/018-fail-unplaceable-runs.md) | 002, 003, 012 | 014, 016 |
| 019 | P1 | ready | [Serve the live run log stream instead of refusing it](tasks/019-serve-run-log-stream.md) | — | — |
| 020 | P1 | ready | [Settle `notin` selector semantics across both evaluators](tasks/020-settle-notin-selector-semantics.md) | — | 014, 018 |

Priority meanings:

- **P0**: correctness, durability, or security boundary; complete before calling
  the NATS transport production-ready.
- **P1**: operational completeness, verification, or migration safety.
- **P2**: quality or scheduling improvement that does not repair a safety boundary.

## Workstreams and Ordering

```text
Claim lifecycle: 001 ─┐
                     ├─→ 011 integration/crash suite
                 010 ─┘

Durability:      002 → 003 → 012
                   └─────→ 011
                 013 (independent asset limits/observability)

Authentication: 004 ─┐
                 005 → 009 ├─→ 011
                 006 ─────┘

Runner contract: 007 ─┐
                 008 ─├─→ 011
                 009 ─┘
                 014 (independent placement improvement)

Operability:     012 ─┐
                 013 ─┴─→ 016 (per-Runner queue view in UI and CLI)

Migration:       safety tasks + operational tasks → 015
```

Tasks in separate workstreams may proceed concurrently when their conflict
metadata does not overlap. A likely conflict is not a dependency; it means agents
should coordinate file ownership or serialize merges.

## Completion Gate

Every task must satisfy its own Definition of Done and normally finish with:

```sh
make audit/postgres     # vet + staticcheck + race tests, incl. the DB-backed ones
git diff --check
```

`make audit/postgres` is what CI runs, and it needs a Postgres —
`make run-postgres-podman`. Plain `go test ./...` and `make audit` silently skip
every test that needs a real transaction or real row locking, so they are not
sufficient evidence for a task in the Durability workstream.

Tasks changing Web UI behavior also run `(cd website && npm test)`. Tasks changing
NATS or distributed lifecycle behavior must include focused integration tests that
exercise the named failure, not only unit tests of the success path. Tasks adding
operator-visible state must land it on both operator surfaces — see
[`CONTEXT.md`](CONTEXT.md#operator-surfaces).

## Backlog Maintenance

- Keep each task self-contained enough for an agent receiving only that task.
- Update status, dependencies, and the completion record as work lands.
- Move newly discovered scope into a separate task instead of silently widening
  an active task.
- Do not recreate these items as flat bullets in `TODO.md`; link here instead.
