# Development and verification

The service uses Go, Node for the React build, Python for the SDK and measurement helpers, and a real Postgres database for integration tests. There is no SQLite or in-memory database fallback in the engine tests. TEST_DATABASE_URL is mandatory; absent configuration fails rather than skipping tests.

Run make setup build after installing the toolchain declared in go.mod. Supply an isolated Postgres URL in TEST_DATABASE_URL, then run make lint test bench. Tests create and remove only their own randomly named schemas. Benchmarks use independently named schemas and reconcile before cleanup. Never point test credentials at a production database. The benchmark entrypoint has no default database URL.

`make bench` records load sweeps and a short real-process chaos campaign. `make bench-long` runs the sustained campaign for its declared wall-clock duration, with recurring worker-transport faults. It does not speed up a simulated clock or relabel virtual time as hours. Raw event streams are gzip-compressed JSONL; reconciliation includes per-workflow latency rows. Worker logs are structured JSONL.

Latency boundaries:

- Enqueue-to-first-claim uses database enqueue and claim timestamps. It excludes waiting to enter admission, HTTP transport and instructions after claim. Request-to-handler-start latency is not measured. It includes queueing and any requested schedule delay. The raw reconciliation also records schedule-adjusted latency.
- Workflow throughput includes admission, the ordered step pipeline, retries, faults and drain. It is not single-handler CPU throughput or an HTTP API benchmark.
- Trace export is disabled in the native benchmark environment; exporter overhead is not measured. The independent Compose test verifies actual OpenTelemetry export.
- The native database and application share this development machine with verification tasks. These are reproducible workload observations, not dedicated-host production capacity claims.

CI runs formatting, vet, static TypeScript checks, Python lint/format checks, race-detector tests, dependency scans, a short process chaos gate, and a clean-clone Compose test including Python-SDK admission, conflict detection, storage-sync delay and real collector output. Long benchmarks and browser video recording are not part of every CI run.

The storage delay is explicitly test-only. compose.chaos.yaml replaces stock Postgres with an instrumented image. A transient flag enables syscall delay, and the harness disables it in a finally block. Scheduled jobs are admitted before activation to isolate worker durability; the initial admission-under-stall failure is retained and documented. Normal Compose never loads this interposer.

The optional embedded service is tested as a real child process through HTTP/SDK admission and clean signal shutdown. Tests of the hard engine protocol additionally exercise stale holders, lock-wait expiry, concurrent completion and rollback after an injected database exception.
