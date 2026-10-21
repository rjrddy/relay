"""Inject actual storage sync latency in the isolated Linux Compose Postgres process."""

import json
import os
import subprocess
import sys
import time
from pathlib import Path
from uuid import uuid4

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "sdk/python"))
from relay_sdk import Client

compose = ["docker", "compose", "-f", "compose.yaml", "-f", "compose.chaos.yaml"]
client = Client("http://127.0.0.1:8080", os.environ["RELAY_API_TOKEN"])
# Admission may correctly return 503 under a storage stall. Stage the immutable scheduled
# jobs before activation so this experiment isolates worker durability, not admission SLOs.
runs = [
    client.enqueue("storage-" + uuid4().hex, i, fail_until=1, max_attempts=8, delay_ms=5000)
    for i in range(12)
]
subprocess.run(compose + ["exec", "-T", "postgres", "touch", "/faults/slow-disk"], check=True)
started = time.monotonic()
try:
    for run in runs:
        while True:
            row = client.run(run["id"])
            if row["run"]["state"] == "done":
                break
            assert row["run"]["state"] != "dead"
            if time.monotonic() - started > 120:
                raise AssertionError("storage delay recovery deadline")
            time.sleep(0.2)
        assert len(row["effects"]) == 3
        assert len({(x["run_id"], x["position"]) for x in row["effects"]}) == 3
finally:
    # This removes only the transient fault flag we just created, not any data directory.
    subprocess.run(
        compose + ["exec", "-T", "postgres", "rm", "-f", "/faults/slow-disk"], check=True
    )
logs = subprocess.check_output(compose + ["logs", "--no-color", "postgres"], text=True)
assert '"fault":"slow_storage_sync"' in logs, "No real fsync call was intercepted"
Path("docs/results/storage-fault.log").write_text(logs)
result = {
    "workflows": len(runs),
    "completed": len(runs),
    "duplicate_effects": 0,
    "elapsed_seconds": time.monotonic() - started,
    "injection": "LD_PRELOAD delays each real fsync/fdatasync call before invoking libc; not a hardware-disk benchmark",
    "raw_result_rows": [client.run(r["id"]) for r in runs],
}
Path("docs/results/storage-fault.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps({k: v for k, v in result.items() if k != "raw_result_rows"}, indent=2))
