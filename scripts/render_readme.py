"""Build result tables only from saved command output; unfinished means not measured."""

import json
from pathlib import Path

root = Path("docs/results")


def read(name: str) -> dict:
    return json.loads((root / name).read_text())


tests = read("tests.json")
loads = [read(f"load-{n}.json") for n in (100, 500, 1000)]
short = read("chaos.json")
long = read("chaos-hour.json") if (root / "chaos-hour.json").exists() else None
video = read("demo-probe.json")["format"]
browser = read("browser.json")
storage = read("linux-storage/storage-fault.json")
rows = "\n".join(
    f"| {r['workflows']} | {r['step_effects']} | {r['workflows_per_second']:.2f} | {r['enqueue_start_p50_ms']:.2f} | {r['enqueue_start_p99_ms']:.2f} | {r['lost_workflows']} / {r['duplicate_effects']} |"
    for r in loads
)
if long:
    campaign = f"{long['workflows']:,} workflows / {long['step_effects']:,} effects over {long['elapsed_hours']:.6f} measured wall-clock hours; {long['lost_workflows']} lost workflows / {long['duplicate_effects']} duplicate effects"
    long_notes = f"The long run lasted {long['elapsed_seconds']:.3f} seconds with {long['workers']} configured steady worker processes. Its p99 enqueue-to-first-claim was {long['enqueue_start_p99_ms']:.2f} ms. Fault timing is recorded in the result, not inferred from simulated time."
else:
    campaign = "not measured — sustained campaign still running"
    long_notes = "Do not treat an in-progress campaign as a completed durability result."
text = f"""# Relay

## Problem

Keep ordered workflow steps recoverable after worker and connection failures without repeating committed internal effects.

**Status: partial — live deployment is outstanding.** [Public repository](https://github.com/rjrddy/relay) · [Demo recording](https://raw.githubusercontent.com/rjrddy/relay/main/docs/results/demo.webm) · [Interview kit](INTERVIEW.md).

## Results

Every result below is rendered from saved command output in [docs/results](docs/results/), with raw per-run timing, reconciliation, events and worker logs. These are synthetic workflows on the recorded development environment, not company usage or production capacity claims.

| Proof | Observed result | Evidence |
|---|---|---|
| Unit / integration / property / race checks | {tests["passed"]} tests passed, {tests["failed"]} failures | [test summary](docs/results/tests.json), [raw events](docs/results/tests.jsonl) |
| Sustained chaos campaign | {campaign} | [campaign](docs/results/chaos-hour.json), [reconciliation](docs/results/chaos-hour-reconciliation.json), [compressed audit events](docs/results/chaos-hour-events.jsonl.gz) |
| Short chaos campaign | {short["workflows"]:,} workflows, {short["lost_workflows"]} lost / {short["duplicate_effects"]} duplicate effects in {short["elapsed_seconds"]:.3f} seconds | [result](docs/results/chaos.json), [raw reconciliation](docs/results/chaos-reconciliation.json) |
| Linux storage-sync fault | {storage["completed"]} of {storage["workflows"]} scheduled workflows completed; {storage["duplicate_effects"]} duplicate effects | [raw rows](docs/results/linux-storage/storage-fault.json), [Postgres log](docs/results/linux-storage/storage-fault.log) |
| Browser recovery and operator flow | lost acknowledgement deduplicated; completed prefix preserved; page errors: {len(browser["page_errors"])} | [browser result](docs/results/browser.json), [original prefix](docs/results/browser-before-replay.json) |
| Actual screen recording | {float(video["duration"]):.2f} seconds | [video](docs/results/demo.webm), [ffprobe output](docs/results/demo-probe.json) |

{long_notes}

Load sweep, including engine admission through all ordered effects and drain:

| Workflows | Step effects | Workflows / second | First-claim p50 ms | First-claim p99 ms | Lost workflows / duplicate effects |
|---|---|---|---|---|---|
{rows}

[Full required gate](docs/results/checks.log) runs `make lint test bench`; [environment/source hashes](docs/results/checks.json) and [database durability settings](docs/results/database-environment.json) describe the measurement environment. Native trace export is disabled; export overhead and HTTP API throughput are **not measured**. First-claim latency uses the stored database enqueue and first-claim timestamps. It excludes waiting to enter admission, HTTP transport and worker instructions after the claim; request-to-handler-start latency is not measured. It includes queueing and requested schedule delay; raw reconciliation also records schedule-adjusted latency. This is a burst/steady synthetic workload, not a hard real-time SLO.

The chaos harness terminates real worker processes after claim and after commit, then repeatedly closes/delays worker-to-Postgres TCP connections. The producer/observer stays directly connected so it can independently reconcile. A process exiting after observing commit is not falsely labeled a network-dropped COMMIT acknowledgement. The separate browser experiment replaces a real successful admission response with a failure, reloads, and proves the retry acknowledges the existing run. The storage experiment delays actual fsync/fdatasync calls, not a simulated timer in a handler; it is not a failing physical-device benchmark.

Caught failures are retained: a stale completion is rejected by the fenced-lease test in raw test output; [the initial real storage-fault gate](docs/results/ci-initial-failure.log) caught an unhandled admission failure. SDK retries now preserve immutable keys. The passing storage test admits scheduled jobs before activating its fault and proves worker durability; it does **not** reinterpret the earlier error as a successful admission-latency test.

![Relay operations console](docs/results/console-desktop.png)

## Architecture

```mermaid
flowchart LR
  S[Go / Python SDKs] --> A[Authenticated Go API]
  U[React ops console] --> A
  A --> P[(Postgres runs / steps / events)]
  C[Versioned dispatch controls] --> P
  P --> W[Leased Go workers / heartbeats]
  W --> F[Owner + epoch + expiry fence]
  F --> T[Atomic effect + step + successor transaction]
  T --> P
  A --> O[OpenTelemetry collector]
  W --> O
  P --> R[Independent SQL reconciliation]
```

## Run it

From a clone with Docker Compose and Python installed:

```sh
python3 scripts/configure.py
docker compose up --build
```

The configuration script creates ignored local secrets without printing them. Open .env locally to copy your operator password, then sign in at http://localhost:8080. Compose seeds synthetic workflows and starts the API, independent workers, Postgres and a trace collector. Normal Compose never loads the storage-fault shim. No external account is required.

For native setup, SDK examples, test-database configuration and benchmark commands, see [development](docs/DEVELOPMENT.md). For the exact recording flow, see [demo steps](docs/DEMO.md). [Optional deployment configuration](deploy/render.yaml) is prepared but **not deployed**.

## Decisions and limits

Read [DESIGN.md](docs/DESIGN.md), [ADRs](docs/adr), [migration provenance](docs/migration/relay.md), [INTERVIEW.md](INTERVIEW.md), [DRILL.md](docs/DRILL.md) and [quality report](docs/QUALITY.md).

- Execution is at least once. The one-effect guarantee applies to uniquely keyed internal Postgres effects committed with step state, not arbitrary HTTP/email/payment tools.
- This is a versioned ordered synthetic pipeline, not a general DAG compiler or arbitrary code-execution service. Scheduling is a one-shot relative delay stored as a database timestamp; cron is absent.
- Admission and control publication serialize on a central row. This is a known bottleneck, and storage stalls can produce bounded retryable unavailability. Idempotent retries are required; no admission SLO is claimed during those stalls.
- Lease epochs survive replay. Completed prefix effects are retained. Bounded attempts can intentionally terminate work in dead letters; a dead letter is not relabeled as success or silently discarded.
- Pause is a transaction barrier for new claims and internal effect commits. It cannot undo already committed work. Rollout changes affect claim eligibility.
- No multitenant isolation, database-replica failover, cross-region consensus, external-effect exactly-once guarantee or automated retention/backup is implemented.
- Operator sessions are local to the API process; SDKs use a separate provisioned bearer token. Hosted TLS and Secure cookies are required. The demo database role can migrate schema; production should separate migration credentials from runtime permissions.
- Async OpenTelemetry export is verified, but spans can be lost on crashes and there is no long-term trace backend. Postgres state/events are authoritative.
- The long campaign uses the same production engine and fault-harness sources as the final implementation; [source equality hashes](docs/results/campaign-source.json) record this boundary. SDK/UI and optional embedded serve mode are separately tested.
- Security scans report no affected Go call paths/imported packages and no frontend advisories in the saved run. The Go module graph still carries an advisory for the unimported OpenPGP package in golang.org/x/crypto; Relay uses scrypt. See [verbose scanner output](docs/results/security-verbose.log). This is not a claim of a zero-advisory module graph.
- Live demo: **not deployed**. The optional free-host mode embeds workers in the web process; processing stops while that instance sleeps, and the database is subject to provider lifetime/storage limits. Hosted performance is not measured. All original repositories remain intact.

## Next

Deploy after choosing a host and persistence policy. Add an outbox with an independently idempotent mock remote sink, tenant-scoped quotas, retention/backup drills, a measured admission-scaling design, a real trace backend and explicitly scoped database-failover experiments. Preserve the current pipeline and failure cases as regression fixtures.
"""
Path("README.md").write_text(text)
print("Rendered README only from saved measurements; sustained campaign complete:", bool(long))
