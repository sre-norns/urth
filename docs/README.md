# Urth documentation

This directory holds design documentation that should remain useful independently of a
particular implementation or release.

## Architecture decision records

Architecture decision records (ADRs) explain the context behind a significant decision,
the decision itself, and its consequences. They are historical records: when a decision
changes, add a new ADR that supersedes the old one instead of rewriting the old context.

| ADR | Status | Decision |
|---|---|---|
| [0001](./adr/0001-resource-oriented-monitoring-platform.md) | Accepted | Model Urth as a resource-oriented monitoring platform with CRD-like resources and distributed probing vantage points. |
| [0002](./adr/0002-worker-authentication.md) | Accepted | Authenticate workers through Runner enrollment, Worker sessions, and Result-scoped run capabilities. |
| [0003](./adr/0003-runner-worker-model.md) | Accepted | Schedule jobs to logical Runner channels and execute them on physical Worker processes. |
| [0004](./adr/0004-nats-communication-backbone.md) | Accepted | Use NATS and JetStream for durable Runner jobs, resource events, and internal communication without making messaging the source of truth. |
