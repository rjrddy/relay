# Relay salvage record

All original repositories remain in projects/ with their `.git` directories, remotes and evidence intact. No archive, migration tag or deletion has occurred.

- P7 src/jobs.py: ported the atomic internal-effect model, bounded admission, fenced attempts, retry and crash-after-claim/commit scenarios. Rewrote SQLite transactions as Postgres row locks, lease epochs, ordered step transitions, bounded attempts, dead letters and audit events. No SQLite runtime is used by Relay.
- X1 src/main.py: retained the principle that delivery receipts and retry/ack uncertainty need a durable dedupe boundary. Replaced Redis Lua/Streams with Postgres transactional effects. The process-exit-after-commit experiment is precisely labeled; it is not falsely described as an actual network-dropped COMMIT acknowledgement.
- X2 src/main.py: adapted stable salted rollout cohorts, kill switches, published configurations and rollback. Added persistent configuration versions, compare-and-set publication and a commit barrier. The new cohort algorithm is a new run-ID-based implementation; compatibility with the old client-ID cohort mapping is not claimed. An offline SDK cache is not part of the dispatch authority.
- M5 src/main.py: used fault-injection and independent history-checking ideas. Implemented a real TCP proxy for worker-to-Postgres partitions/delay and independent SQL reconciliation of receipts, transitions and effects. No fixed-coordinator replicated KV implementation or consensus claim is carried into Relay.

Go is the implementation language specified for this flagship. All old Python algorithms were re-expressed in the new transaction model; no sibling checkout or shared virtualenv is required. Original public repository URLs and source commits are in origins.json. Current measurements are generated from Relay, never copied from old project evidence.

The fault topology matters: the producer/observer connects directly to the isolated Postgres schema, while workers connect through the fault proxy. The storage test runs a real Linux Postgres process with a test-only library delaying actual sync calls. Neither experiment claims a database-cluster consensus partition or a physical failing-disk benchmark.
