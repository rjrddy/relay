# Operator sessions and machine-client idempotency

Context: browser mutations and SDK ingestion have different authentication and failure-recovery needs.

Options: public anonymous demo, shared token everywhere, external identity provider, or a small single-operator session flow plus machine token.

Decision: salted scrypt password verifier, random expiring HttpOnly SameSite strict cookies, CSRF proofs on cookie mutations, and a separate environment-provisioned bearer token for SDKs. Request sizes, session counts and login/write/read rates are bounded. Hosted deployment uses Secure cookies and TLS. Generate ignored local secrets with exclusive file creation and restrictive permissions.

Go and Python SDKs retry only safe enqueue requests on transient responses/transport failure, keeping the same key and payload. Conflicts, validation errors and authentication failures are returned without retry. Control mutations are not automatically retried. The browser keeps an unresolved immutable request in session storage and requires acknowledgement before generating its next key; the recording demonstrates a response replaced by a failure after a real commit, followed by a reload and deduplicated retry.

Tradeoffs: no multi-user roles, tenant separation, persistent sessions or secret rotation endpoint. Restart invalidates sessions. IP-based budgets are a demo limiter and may be shared behind proxies. The SDK token is powerful; it is not a scoped enterprise capability token. Payloads in this project are bounded synthetic numeric fields, not real records.
