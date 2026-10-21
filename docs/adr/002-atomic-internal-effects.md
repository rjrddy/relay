# One committed internal effect, not exactly-once execution

Context: a worker can commit and lose the response, then be retried or reclaimed.

Options: acknowledge separately from side effects; use a remote sink without dedupe; or commit the effect and state transition together.

Decision: the built-in synthetic pipeline writes effects keyed by workflow and step position inside the same transaction that marks the step done and releases its successor. A stale or duplicate completion cannot cross the fence. A database error after effect insertion rolls the effect and transition back together. Concurrency tests explicitly attempt duplicate completion.

Tradeoffs: this is a real guarantee for this Postgres-resident synthetic effect model. HTTP, email and payment providers would need their own stable idempotency keys or an outbox plus recipient dedupe protocol. Handlers may execute repeatedly; do not call their execution exactly once. The workflow definition is an ordered versioned pipeline, not an arbitrary DAG language.
