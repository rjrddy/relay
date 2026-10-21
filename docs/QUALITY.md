# Relay quality checkpoint

Status: **partial — live deployment and complete hour audit export are outstanding**. The measured local demo recording is complete. No hosting account was created or service deployed. All old repositories remain intact; archive/tag/deletion is a later phase.

| Required check | Result | Evidence |
|---|---|---|
| Hard core written and explained | PASS | internal/engine, migrations/schema.sql, docs/DESIGN.md |
| One-command run from fresh clone | PASS on Linux CI | Compose logs and HTTP/SDK/storage checks; native independent clone also passes |
| UI shows live behavior | PASS | docs/results/browser.json, desktop/mobile screenshots and demo.webm |
| Unit/integration/property/chaos tests | PASS | race-detector test events, process/transport campaign, rollback and fencing tests |
| Benchmark numbers saved | PASS | load sweeps, completed hour protocol reconciliation; full audit export failed, recovered prefix retained |
| Demonstrated caught failure | PASS | stale completion rejection in raw tests; retained initial storage-fault admission failure; real lost-ack browser recovery |
| ADRs | PASS | docs/adr/ |
| INTERVIEW.md and DRILL.md | PASS | code-grounded walkthrough, tradeoffs, failure modes and follow-up answers |
| CI green | PASS for recorded source; verify final push in workspace record | docs/results/ci-code.json/log; final workspace RELAY_VERIFICATION.json |
| No unmeasured result claims | PASS on manual review | generated README, explicit measurement boundaries, independent raw-event/latency audit |
| Completed screen recording | PASS | demo.webm and measured media duration in demo-probe.json |
| Full hour audit export | FAIL | hour-export-status.json; original command exited 1 after reconciliation; paginated fix passes short run, hour not rerun |
| Hosted live demo | FAIL / not deployed | deploy/render.yaml is preparation, not a live URL |

The dependency gate reports no affected Go call paths/imported packages and no frontend advisories in the saved run. There is a module-scope advisory for an unimported OpenPGP package in golang.org/x/crypto; Relay uses scrypt. The verbose report is retained. No advisory is silently ignored or relabeled as a clean module graph.

Limits include an ordered synthetic pipeline, one authoritative Postgres primary, a central admission/configuration bottleneck, bounded retries with visible dead letters, no arbitrary remote-effect exactly-once guarantee, no tenant isolation or automated backup/retention, and no hosted performance claim. The worker-transport fault and real storage-sync-call delay are precisely scoped; neither is a multi-region database consensus or physical-device benchmark.

The campaign source record proves that the final production engine, worker and fault-proxy files match the sustained run; export-only harness changes are recorded separately. SDK/UI and optional embedded serve mode are independently tested. Stop for review before Pulse.
