# Observability

The authenticated console reads durable Postgres state. It displays running/delayed/terminal counts, committed step effects, retry transitions, queue latency, step epochs, owner IDs and trace-linked events. Metrics are available at authenticated /api/metrics. The latency histogram uses the recent raw samples returned by the API; aggregate percentiles include the persisted history and scheduled waiting.

The Compose OpenTelemetry collector accepts OTLP/HTTP from the API and workers and uses its debug exporter. Inspect `docker compose logs otel` to see actual workflow.step spans, their trace/parent IDs and step attributes. Linux CI saves this output. This is a working exporter pipeline, not a claim of long-term trace retention or a hosted tracing dashboard.

State/event transactions are authoritative even when the exporter is unavailable. Span loss on crash is possible. Structured logs contain identifiers, epochs, attempts and outcomes; arbitrary payloads, client IPs, passwords and API tokens are excluded. Trace context is propagated through the durable run record into later worker processes.

Event/trace retention and production database backups are not automated. The free-host configuration does not provision a collector automatically; enable OTLP export only for a chosen collector endpoint.
