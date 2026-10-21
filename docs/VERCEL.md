# Vercel

Public evidence site: https://relay-brown-six.vercel.app. This is currently a recording and measured-results viewer, not the interactive engine.

The user will provide an existing PostgreSQL database by setting DATABASE_URL in the Relay project's Production environment:
https://vercel.com/raj-reddys-projects/relay/settings/environment-variables

Use a TLS-capable connection string and a role allowed to create a separate schema. The hosted entrypoint creates only relay_vercel_v1 and puts its workflow/auth tables there; it does not migrate the public schema. Runtime authentication uses revocable hashed sessions in this schema. Migration privileges should be split from runtime privileges for a production system.

Operator credentials have been provisioned without printing their values. The matching local copy is in ignored .env.vercel-operator with restrictive file permissions. Open it locally to retrieve RELAY_PASSWORD. Never commit or paste the file into public logs. RELAY_API_TOKEN is the machine SDK secret.

After DATABASE_URL is present, run `python3 scripts/prepare_vercel_live.py --enable`, then `npx vercel@65.0.0 deploy --prod --yes`. The script refuses to activate without the required database environment and preserves existing configured credentials. Commit the activated vercel.json so subsequent GitHub auto-deploys retain live mode.

The live config uses Vercel's current container runtime and a separate worker daemon. It serves the evidence page at /, the actual console at /console/, and authenticated APIs at /api/. Daemons and container runtime are provider beta features and remain subject to account quotas. No paid plan, database provider account or unrelated project is created automatically. The idle worker still polls Postgres; review provider quotas before leaving it running indefinitely.

Current checks prove the entrypoint locally and test the image in CI; they do not claim successful external database activation. Keep this distinction until public login, enqueue, completion and replay are verified against the deployed service.

Official references checked during implementation: https://vercel.com/docs/functions/runtimes/container, https://vercel.com/docs/daemons/config-reference, https://vercel.com/docs/services.
