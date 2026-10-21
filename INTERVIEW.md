# Relay interview guide

## Two-minute pitch

Relay is a durable workflow engine whose hardest problem is uncertainty after a worker or connection dies. A handler may execute again, so I do not promise exactly-once execution. Instead, Postgres assigns leased steps with monotonically increasing epochs, and a current owner must pass a fence before committing a uniquely keyed internal effect and its state transition together. The API supports immutable idempotency keys, scheduled work, capacity backpressure and versioned dispatch controls. The React console exposes persisted runs, retries, dead letters and trace-linked history. Both SDKs preserve the request across bounded retries, and the browser preserves an unresolved admission across reloads. The proof is raw Postgres reconciliation after actual worker exits, worker-transport partitions and a separate Linux storage-sync delay experiment.

## Ten-minute walkthrough

Start in migrations/schema.sql: runs are immutable inputs, steps are the state machine, effects are the sink and events explain transitions. Trace engine.Enqueue through its admission lock, fingerprint check and capacity check. Walk through Claim's SKIP LOCKED query and epoch increment. Then examine verify: acquire the row lock before rechecking the current owner, epoch and database-clock expiry. Show why a stale heartbeat cannot revive a lost lease.

Next follow worker.handle: extract the stored trace context, heartbeat while working, read the completed predecessor and commit or retry using the same fence. Explain why effects and the successor transition share one transaction. Show the database-exception rollback test and concurrent completion test.

Move to control-plane semantics: pause publication is ordered against claims/effect commits by the shared/exclusive control lock; staged rollout affects eligibility, while rollback is a new version. Manual replay retains the completed prefix and never resets the epoch. Then open the console and the actual demo recording. Finish with docs/results and the precise benchmark/fault boundaries. Use the measured README tables; if a deployment or performance metric has no saved run, call it not measured.

## Five tradeoffs

- Postgres row locks instead of a separate broker: one durable transaction boundary, at the cost of database polling and contention.
- Serialized admission/control instead of approximate per-client capacity: strict capacity and pause semantics, at the cost of a bottleneck and retryable unavailability during storage stalls.
- Internal transactional effects instead of arbitrary remote side effects: a demonstrable correctness guarantee, without falsely promising external exactly-once delivery.
- Ordered versioned steps instead of a general DAG language: smaller state space and defendable failure behavior, without arbitrary workflow authoring.
- In-process operator sessions and optional embedded demo workers: straightforward local/free-demo operation, without enterprise identity or independent failure isolation in that optional mode.

## Five failure modes

- Worker exits after claim: the lease expires, another owner increments the epoch and retries. Exhausted attempts become a visible dead letter rather than silently disappearing.
- A stale worker resumes: completion, retry and heartbeat verify the current fence. The lease check occurs after taking the row lock so a long wait cannot revive an expired holder.
- Database transport disappears: worker operations have deadlines, uncommitted transactions roll back, and uncertain committed operations are reconciled from durable state. No local success counter substitutes for the database.
- An enqueue acknowledgement is lost: SDK retries and UI recovery keep the same immutable key/payload. The browser recording replaces a real successful response with a failure, reloads and proves that the retry acknowledges the existing run.
- Storage sync stalls: throughput degrades, leases may need recovery and admission can return a retryable error. The passing storage test stages jobs before activation to isolate worker correctness; the earlier admission failure is retained. This is not a physical failing-disk capacity claim.

## Fifteen likely questions and answers

1. **What exactly is guaranteed?** One committed internal effect per workflow position, atomically coupled to the done transition. Handlers can execute repeatedly. External sinks need idempotency or an outbox protocol.
2. **Why both owner and epoch?** An owner identifies a process; an epoch distinguishes successive leases even if a process identity is reused. The epoch survives manual replay and increases on every claim.
3. **Why not check expiry before taking the row lock?** A worker could wait behind another transaction until its lease expired, then act on an obsolete pre-wait check. verify locks first and reevaluates with the database clock.
4. **What is the linearization point?** The Postgres transaction commit for admission or effect/state transition. A worker-local method return is only its observation of that commit and can be uncertain under a connection failure.
5. **Why SKIP LOCKED?** Independent workers can claim available rows without waiting on another claim. It is not a global fairness guarantee; ordering is an operational preference.
6. **What if pause races completion?** They lock the same control row in shared versus exclusive modes. A completion that entered first can commit before pause; after pause publication commits, later internal effect commits are blocked until resume.
7. **Why include retry policy and delay in the fingerprint?** Reusing a key with a different schedule or budget would otherwise silently change the meaning of the logical request. A retry preserves the originally stored schedule instead of delaying it again.
8. **What happens when the queue is full?** New logical runs are rejected. A matching existing key is still acknowledged because dedupe is checked before capacity. Delayed and running workflows both consume admission capacity.
9. **Why does replay not erase earlier effects?** Only the failed position is requeued. The completed prefix remains done and uniquely keyed, and the cumulative fence epoch is retained. The recording compares the original prefix effect and its commit timestamp.
10. **What does the chaos experiment partition?** Worker-to-database TCP traffic. The producer/observer remains directly connected so it can keep admitting and independently reconcile. This is not a database-cluster quorum partition or replica failover test.
11. **What does slow disk mean here?** A test-only Linux library delays actual fsync/fdatasync calls and still invokes libc. Saved server logs prove interception. It measures behavior under sync-call latency, not a real failing block device.
12. **Can the configuration row become a bottleneck?** Yes. The observed admission-under-stall failure demonstrates that cost. I chose strict centralized admission/pause semantics and bounded retryable errors for this scope; a sharded capacity protocol is future work.
13. **How are traces connected across processes?** Enqueue persists W3C trace context with the run, and workers create child spans from it. Durable events store trace/span IDs. Async exporter loss does not lose workflow state.
14. **What do the performance numbers include?** The saved load results include engine admission through all ordered effects and drain, not HTTP API transport. First-claim latency uses database enqueue/claim timestamps, excluding admission-lock waiting, HTTP transport and post-claim worker instructions. It includes schedule waiting; raw rows also expose schedule-adjusted latency. Request-to-handler-start is not measured. Native trace export overhead and hosted capacity are not measured.
15. **How would you make this a production platform?** Separate migration/runtime DB roles, introduce tenant and workload isolation, define retention/backup and external-effect protocols, benchmark contention under larger histories, add operational SLOs and perform explicitly scoped database failover campaigns. Do not infer those guarantees from this demo.

## What I would do with three more months

Build an outbox and an independently idempotent mock remote sink; add tenant-scoped quotas and authorization; version handler registrations and workflow definitions explicitly; profile and shard the admission bottleneck with a proven capacity protocol; implement retention and backup/recovery drills; add a real trace backend; and run longer host/storage/failover campaigns against declared SLOs. Keep the current ordered pipeline as the regression baseline.
