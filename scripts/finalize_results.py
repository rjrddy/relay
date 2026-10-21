"""Independently audit completed measurements, compress raw logs, and write a quality manifest."""

import collections
import gzip
import hashlib
import json
import math
import shutil
import subprocess
from pathlib import Path

out = Path("docs/results")
long = json.loads((out / "chaos-hour.json").read_text())
reconciled = json.loads((out / "chaos-hour-reconciliation.json").read_text())
assert long["elapsed_seconds"] >= 3600, "An hour-long result must measure at least an hour"
for key in (
    "dead",
    "pending",
    "missing_effects",
    "orphan_effects",
    "incorrect_receipts",
    "duplicate_effects",
):
    assert reconciled[key] == 0, (key, reconciled[key])
assert reconciled["runs"] == reconciled["done"] == long["workflows"]
assert reconciled["effects"] == long["step_effects"] == long["workflows"] * 3
counts = collections.Counter()
run_ids = set()
last_seq = 0
with gzip.open(out / "chaos-hour-events.jsonl.gz", "rt") as stream:
    for line in stream:
        row = json.loads(line)
        assert row["seq"] > last_seq
        last_seq = row["seq"]
        counts[row["kind"]] += 1
        run_ids.add(row["run_id"])
assert counts["enqueued"] == long["workflows"]
assert counts["committed"] == long["step_effects"]
assert counts["dead"] == 0 and len(run_ids) == long["workflows"]
values = sorted(row["enqueue_start_ms"] for row in reconciled["latencies"])


def quantile(q: float) -> float:
    position = q * (len(values) - 1)
    low, high = math.floor(position), math.ceil(position)
    return values[low] + (values[high] - values[low]) * (position - low)


assert math.isclose(quantile(0.5), long["enqueue_start_p50_ms"], abs_tol=1e-8)
assert math.isclose(quantile(0.99), long["enqueue_start_p99_ms"], abs_tol=1e-8)
audit = {
    "workflows": len(run_ids),
    "event_counts": dict(counts),
    "last_sequence": last_seq,
    "latency_rows": len(values),
    "recomputed_p50_ms": quantile(0.5),
    "recomputed_p99_ms": quantile(0.99),
    "reconciliation_invariants": "pass",
    "source_boundary": "production engine and worker-transport campaign",
}
(out / "independent-audit.json").write_text(json.dumps(audit, indent=2) + "\n")

# Retain originals locally; publish lossless compression rather than large active log files.
cache = Path(".cache/raw-worker-logs")
cache.mkdir(parents=True, exist_ok=True)
for path in sorted(out.glob("*.jsonl")):
    if not path.name.startswith(("load-", "chaos-")):
        continue
    target = path.with_suffix(path.suffix + ".gz")
    with path.open("rb") as source, target.open("wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed:
            shutil.copyfileobj(source, compressed)
    shutil.move(str(path), cache / path.name)
for name in ("chaos-events.json", "browser-failure-state.json"):
    path = out / name
    if path.exists():
        shutil.move(str(path), cache / name)

clone = Path("/tmp/relay-fresh-clone")
shutil.copyfile("/tmp/relay-fresh-clone.log", out / "fresh-clone-install.log")
shutil.copyfile("/tmp/relay-fresh-final.log", out / "fresh-clone-final.log")
clone_tests = json.loads((clone / "docs/results/tests.json").read_text())
assert clone_tests["passed"] and not clone_tests["failed"]
clone_head = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=clone, text=True).strip()
current = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
paths = [
    "cmd",
    "internal",
    "migrations",
    "sdk",
    "apps/web/src",
    "Dockerfile",
    "compose.yaml",
    "compose.chaos.yaml",
    "go.mod",
    "go.sum",
]
subprocess.run(["git", "diff", "--exit-code", clone_head, current, "--", *paths], check=True)
(out / "fresh-clone.json").write_text(
    json.dumps(
        {
            "clone_command": "git clone --no-local /Users/rajreddy/dev/outreach/flagships/relay /tmp/relay-fresh-clone",
            "initial_command": "make setup build lint test",
            "final_command": "make build lint test",
            "verified_head": clone_head,
            "application_sources_match_current": True,
            "tests": clone_tests["passed"],
            "exit_code": 0,
            "dependencies": "independent virtualenv, node_modules, GOPATH and GOCACHE",
            "docker": "real Linux Compose verification recorded separately",
        },
        indent=2,
    )
    + "\n"
)

quality = """# Relay quality checkpoint

Status: **partial — live deployment is outstanding**. The measured local demo recording is complete. No hosting account was created or service deployed. All old repositories remain intact; archive/tag/deletion is a later phase.

| Required check | Result | Evidence |
|---|---|---|
| Hard core written and explained | PASS | internal/engine, migrations/schema.sql, docs/DESIGN.md |
| One-command run from fresh clone | PASS on Linux CI | Compose logs and HTTP/SDK/storage checks; native independent clone also passes |
| UI shows live behavior | PASS | docs/results/browser.json, desktop/mobile screenshots and demo.webm |
| Unit/integration/property/chaos tests | PASS | race-detector test events, process/transport campaign, rollback and fencing tests |
| Benchmark numbers saved | PASS | load sweeps, completed hour campaign, raw reconciliation and compressed audit events |
| Demonstrated caught failure | PASS | stale completion rejection in raw tests; retained initial storage-fault admission failure; real lost-ack browser recovery |
| ADRs | PASS | docs/adr/ |
| INTERVIEW.md and DRILL.md | PASS | code-grounded walkthrough, tradeoffs, failure modes and follow-up answers |
| CI green | PASS for recorded source; verify final push in workspace record | docs/results/ci-code.json/log; final workspace RELAY_VERIFICATION.json |
| No unmeasured result claims | PASS on manual review | generated README, explicit measurement boundaries, independent raw-event/latency audit |
| Completed screen recording | PASS | demo.webm and measured media duration in demo-probe.json |
| Hosted live demo | FAIL / not deployed | deploy/render.yaml is preparation, not a live URL |

The dependency gate reports no affected Go call paths/imported packages and no frontend advisories in the saved run. There is a module-scope advisory for an unimported OpenPGP package in golang.org/x/crypto; Relay uses scrypt. The verbose report is retained. No advisory is silently ignored or relabeled as a clean module graph.

Limits include an ordered synthetic pipeline, one authoritative Postgres primary, a central admission/configuration bottleneck, bounded retries with visible dead letters, no arbitrary remote-effect exactly-once guarantee, no tenant isolation or automated backup/retention, and no hosted performance claim. The worker-transport fault and real storage-sync-call delay are precisely scoped; neither is a multi-region database consensus or physical-device benchmark.

The campaign source record proves that the final production engine and fault-harness files match the sources under the sustained run. SDK/UI and optional embedded serve mode are independently tested. Stop for review before Pulse.
"""
Path("docs/QUALITY.md").write_text(quality)
manifest = {
    "source_head": current,
    "files": {
        str(p.relative_to(out)): {
            "sha256": hashlib.sha256(p.read_bytes()).hexdigest(),
            "bytes": p.stat().st_size,
        }
        for p in sorted(out.rglob("*"))
        if p.is_file() and p.name != "manifest.json" and "demo/" not in str(p.relative_to(out))
    },
}
(out / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
print(json.dumps(audit, indent=2))
print("Saved quality report, independent audit, source comparison and evidence manifest.")
