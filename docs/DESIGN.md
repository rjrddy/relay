# Relay: durable steps under uncertain outcomes

The hard case is not a clean retry. A worker can lose its database connection after committing an effect but before observing success. A second worker can then reclaim an expired lease while the first worker is still alive. The design must distinguish repeated execution from repeated effects.

## State and commitment

A workflow has a versioned definition and an immutable, canonically encoded synthetic input. Enqueue holds a short queue-control row lock, checks the idempotency key and fingerprint before capacity, and atomically creates the run and ordered steps. The queue capacity counts nonterminal runs, including delayed work. All steps exist from enqueue; only the first is eligible initially.

Claims use Postgres `FOR UPDATE SKIP LOCKED`. Claiming increments a monotonically increasing epoch, sets an owner and a database-clock lease deadline, and records an attempt. Reclaim is permitted only after expiration. Attempts are bounded, including crash/reclaim attempts. A heartbeat can extend only an unexpired current lease. Completion locks the step, then verifies state, owner, epoch and unexpired lease before inserting a uniquely keyed effect and advancing the workflow in the same transaction. A stale holder cannot finish, retry or heartbeat. Retry backoff is capped, exponential and deterministically jittered from workflow, step and attempt. A delayed workflow cannot start before its database-clock schedule.

The unique effect key is `(run_id, step_position)`. The built-in synthetic workflow writes its effects inside the same Postgres transaction as the step transition. This gives one committed internal effect per completed step, not universal exactly-once execution. Real HTTP/email/payment tools require an idempotent downstream sink or a transactional outbox plus dedupe protocol. No external service is called by the synthetic workflow.

## Operations and ordering

Versioned rollout settings and a kill switch live in Postgres. Claims and commits take a shared control lock; configuration updates take an exclusive one. Once pause commits, no later claim or completion can cross that boundary. Already committed work is not undone. Stable run cohorts make rollout membership deterministic. Backpressure bounds admitted unfinished runs; completed history and trace/event retention remain operational limits.

A failed step enters a dead letter and blocks later steps. Manual replay requeues only that failed position while preserving completed effects and increasing epochs on future claims. It does not rerun a successful workflow or silently change its input. Audit events record actor, attempt, epoch, outcome and trace identifiers without arbitrary payloads or secrets.

## Architecture

A Go binary supplies migration, seed, API and worker commands. Postgres is authoritative. React polls persisted run, step, event and metric views; no UI counter pretends to be durable engine state. API authentication uses a salted password verifier, random expiring HttpOnly sessions and anti-CSRF proofs. SDKs use a separate environment-provisioned bearer token. Login and mutation rates and request bodies are bounded. Local Compose binds the UI to loopback and keeps the database on its private network.

OpenTelemetry trace context crosses enqueue into stored runs and worker step spans. The collector exports spans to its configured sink; the UI also shows persisted trace IDs and timeline events. Structured logs carry identifiers and outcomes, not synthetic input payloads. Queue latency is enqueue-to-first-claim, separated from schedule delay and retry execution latency.

## Failure proof and limits

Tests must use real Postgres for concurrency, fencing, idempotency and rollback assertions. Chaos must kill actual worker processes, close proxy connections to partition worker DB access, and delay real DB traffic. A delayed connection is not proof of a physical disk fault; that part must remain explicitly partial unless exercised against the storage layer. Benchmarks save raw per-run timestamps and reconciliation rows. A sustained campaign's wall-clock duration is reported as measured, never relabeled as hours of simulated work.

There is no arbitrary DAG compiler, distributed consensus layer, multitenant isolation or general external-effect exactly-once guarantee. The first built-in workflow is an ordered synthetic pipeline. The design favors a small defendable protocol over a large workflow DSL. Postgres transaction/lock semantics, not worker-local memory, establish correctness.
