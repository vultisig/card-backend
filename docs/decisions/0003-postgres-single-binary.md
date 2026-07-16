# 0003 — PostgreSQL + single binary

Status: PROPOSED · 2026-07-16 — CTO input wanted

## Context

The service needs durable storage for the vault↔issuer mapping, an event-sourced transaction cache, a rewards/vesting ledger, and compiled policy state. It also has webhook-driven background work (push fan-out, reconciliation, autopilot triggers).

## Decision (proposed)

- **PostgreSQL** as the only datastore. Ledger-shaped data with transactional integrity requirements; queue needs are modest and can start as Postgres tables (`FOR UPDATE SKIP LOCKED`) rather than a broker.
- **Single binary** (`cmd/card-backend`) serving HTTP and running background workers, split only when scale or deploy topology forces it.

## Consequences

- One system to operate, back up, and reason about transactionally (rewards accrual and txn-cache writes commit atomically).
- Deferred: message broker, separate worker deployment, read replicas. Revisit when webhook volume or push fan-out latency demands it.
- Open questions for review: managed Postgres choice, migration tooling preference (goose/atlas/tern), and whether the org standard dictates otherwise.
