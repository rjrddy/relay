# Postgres leases and fencing

Context: a paused or disconnected worker may resume after another worker has reclaimed its job.

Options: worker-local state, Redis delivery acknowledgements, or transactional Postgres state with lease epochs.

Decision: use SKIP LOCKED claims, database-clock leases, owner plus monotonically increasing epoch, and a row lock before rechecking expiration. Heartbeats cannot revive an expired epoch. Completion and retry verify the same fence. Attempts include crash/reclaim claims and eventually become a visible dead letter.

Tradeoffs: correctness is anchored in one Postgres primary, not a worker mutex. Claim fairness is approximate and there is no database failover protocol. Polling and control-row locking cost database work even when idle. Lease length trades fast crash recovery against tolerance for stalls. No arbitrary code execution time or external side effect is covered by this lease alone.
