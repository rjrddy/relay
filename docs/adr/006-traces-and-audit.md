# Trace correlation and durable event history

Context: operators need to explain a run even when workers die or a trace exporter is unavailable.

Options: tracing only, database audit only, or durable events linked to standard trace context.

Decision: persist workflow/step/attempt/epoch events in the same state transactions, attach trace and span IDs, and propagate W3C trace context from API enqueue into workers. Export actual OpenTelemetry step spans to a collector. The demo collector uses a debug exporter; saved Linux logs show received worker spans. The UI shows persisted event timelines and queue metrics.

Tradeoffs: span export is asynchronous and may lose spans during process death; it is not the durable record and is not atomically committed with the workflow. There is no claim of a long-term trace storage backend or Jaeger UI. The durable event history remains authoritative. Logs exclude passwords, tokens, client IPs and arbitrary payload bodies. History/event retention and database backups remain operational work.
