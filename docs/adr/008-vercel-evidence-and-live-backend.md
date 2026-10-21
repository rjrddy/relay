# Vercel publication and live backend boundary

Context: Vercel hosting is now explicitly requested. The existing Go engine needs externally persistent Postgres and workers; its local SQLite-free design cannot use a static deployment as its database.

Options: display the existing authenticated console with failing API calls; invent a browser simulation; publish the actual recording and saved measurements while preparing live services.

Decision: publish recorded evidence at https://relay-brown-six.vercel.app with clear status, real video, actual load rows and a searchable subset of the saved SQL reconciliation. The existing interactive console and engine stay in the repository. GitHub is connected for automatic deployments. Current Vercel docs support Go services and daemons; live service deployment requires hosted Postgres and provisioned credentials. No third-party account or paid plan was created.

Tradeoffs: the public site is useful immediately and honest about its scope, but visitors cannot submit new durable workflows there yet. This remains a partial live demo. The hour audit export gap remains independent of hosting.

Sources: https://vercel.com/docs/functions/runtimes/go and https://vercel.com/docs/daemons, checked during implementation.

Live preparation now includes cmd/hosted, Dockerfile.vercel and deploy/vercel-live.json. The hosted entrypoint explicitly selects its own relay_vercel_v1 schema and serializes schema initialization. Hashed cookie sessions live in a separate auth table so API instances share login/logout. The production config runs a dedicated worker daemon; worker progress does not depend on an open browser tab. Native entrypoint smoke and shared-session tests passed. External activation remains pending the user's DATABASE_URL; the static evidence site stays active meanwhile.
