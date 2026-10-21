# Bounded retries and replay without erasing history

Context: transient errors need retries, permanent errors need an operator, and a replay must not repeat a completed prefix.

Options: retry indefinitely, restart the entire workflow, or preserve completed steps and replay only the failed position.

Decision: capped exponential backoff with deterministic jitter, bounded per-cycle attempts, persistent retry/dead events, and manual replay of the dead step only. Replay resets the cycle attempt budget but never resets the lease epoch. Synthetic failure injection uses the cumulative step epoch so a demo that fails the initial claims can recover on replay without mutating its immutable input.

Tradeoffs: accepted work may legitimately end dead; that is a terminal result, not silent loss or success. Repeated manual replay is an explicit operator action. A permanently failing input stays permanently failing. There is no automatic compensation, arbitrary workflow patching, recurring cron expression or DAG editing. Enqueue schedule/delay is persisted using the database clock.
