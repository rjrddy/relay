# Independent workers locally; optional embedded workers for a free demo

Context: the local system needs realistic process failure boundaries while the requested hosted example must fit a free web-service deployment shape.

Options: always embed workers, pay for separate background workers, or support both explicit modes using the same worker implementation.

Decision: normal Compose starts independent worker processes alongside the API, Postgres and an OpenTelemetry collector. The optional Render blueprint embeds workers in the web process and uses a separate managed Postgres database. Startup migrations and idempotent synthetic seeding are enabled for that demo. No deployment or account creation is performed by a Git commit.

Tradeoffs: embedded mode couples API and worker lifetime. When a free web instance sleeps, processing stops until it wakes; leases and jobs remain in Postgres, subject to the provider's database lifetime/storage policy. This is not an always-on production deployment. The runtime database role has migration privileges in the demo; a production setup should separate migration and runtime roles. Free-host latency, uptime and durability are not measured.
