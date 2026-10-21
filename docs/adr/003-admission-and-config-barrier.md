# Serialized admission and versioned dispatch controls

Context: concurrent producers must respect capacity, idempotency and a pause boundary without local caches disagreeing.

Options: approximate per-producer counters; distributed rate/capacity coordination; a short authoritative control-row transaction.

Decision: serialize admission and configuration on one control row. Check an existing idempotency fingerprint before capacity so retries still work when full. Claims and effect commits share-lock the control row; pause publication takes an exclusive lock. Rollout eligibility uses a stable salted SHA256 cohort of the run ID. Configuration updates compare expected versions; rollback republishes old settings as a new revision.

Tradeoffs: this row is an intentional throughput and availability bottleneck. Shared claim locks also incur database work while idle. The saved storage-fault failure demonstrated that slow sync calls can cause admission to return a retryable service-unavailable response. SDK retries preserve immutable requests and are bounded; the UI preserves an unresolved admission through a reload. No admission-latency SLO is promised during a storage stall. The passing storage experiment stages scheduled jobs before activation to isolate worker durability. It does not hide or reinterpret the earlier admission failure.

Once pause commits, later claims and internal effect commits cannot pass the barrier. Work committed before pause remains committed. Rollout changes govern new claims; they do not undo completed work. Heartbeats and error/dead-letter bookkeeping are not external effects and may still occur while paused.
