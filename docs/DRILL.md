# Hard follow-up drill

Each answer is grounded in the code and saved experiments. Hypothetical extensions are labeled as future work.

1. **A worker freezes beyond its lease, then resumes. Can it write?** Only if it still passes the current owner/epoch/unexpired check after taking the step lock. A newer epoch fences it permanently.
2. **What if the database clock moves backward?** Lease expiration can be delayed, hurting liveness. Once an epoch is reclaimed, monotonic fencing still rejects the older epoch. This is not a clock-independent consensus protocol.
3. **What if the first claim succeeds but its response is lost?** The lease remains in the database. Recovery eventually reclaims it; the handler may execute again, but internal effect/state commit remains atomic and fenced.
4. **What if completion commits but the worker never sees success?** The done state and effect are already durable. Another claim skips that done position; a repeated completion cannot insert another keyed effect.
5. **What if the effect insert succeeds but the step update fails?** The transaction rolls both back. The injected trigger test checks that no orphan effect escapes.
6. **Why not increment an in-memory completion counter?** It would diverge on crashes and would not prove durability. Reconciliation reads actual persisted runs, steps and effects independently.
7. **Why count delayed jobs toward capacity?** They are accepted unfinished obligations. Excluding them would let a client fill the database with future work while appearing under capacity.
8. **Can a matching retry be rejected when capacity is full?** The existing fingerprint is checked first, so it is still acknowledged. A changed payload with that key is a conflict.
9. **Does a rollout percentage change cancel already running work?** No. It controls new claim eligibility. Use the kill switch to block later internal effect commits; already committed effects are preserved.
10. **Why version rollback as a new publication?** It preserves audit history and compare-and-set semantics. An operator cannot silently overwrite a newer configuration with an old browser snapshot.
11. **Could a retry create an additional run after a page reload?** The UI stores its unresolved immutable request in session storage. The recording demonstrates that a retried lost acknowledgement returns the existing ID with no new admission.
12. **What if the user edits fields while an admission is unresolved?** They are disabled until acknowledgement. The retry uses the original request snapshot rather than a new payload with the same key.
13. **Why no automatic retries for all control writes?** Some control actions are not idempotent publications. The caller should read current state/version after uncertainty rather than replaying a stale operation blindly.
14. **How do you stop poison work?** Bound attempts and record a dead letter. Manual replay is explicit and does not mutate input or reset the fence epoch. Permanently failing input does not become successful by relabeling it.
15. **How would you handle external email exactly once?** I would not promise it from this engine alone. Use a stable external idempotency key or an outbox with recipient-side dedupe and reconciliation.
16. **What is the largest correctness risk in changing the schema?** Breaking the atomic relationship among effect, step transition and successor release. Migrations and regression tests must preserve that boundary before adding features.
17. **What limits the current query/retention design?** Admission/control contention and growing history. There is a partial index for unfinished runs, but no retention service, tenant partitioning or production sizing claim.
18. **Can tracing be used as the recovery log?** No. Export is asynchronous and spans can be lost on process death. Postgres events and state are authoritative.
19. **What does the long campaign not establish?** It does not prove physical disk reliability, multi-region replication, database failover, remote-side-effect idempotency, internet-scale auth or a hosted latency SLO. Its actual duration and fault topology are recorded.
20. **Why support embedded workers at all?** To provide a valid optional free web-demo shape. Separate workers remain the normal Compose mode; embedded mode deliberately shares the API lifetime and stops processing when the free instance sleeps.
