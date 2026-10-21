"""Save reproducible command output and nonsecret environment metadata."""

import hashlib
import json
import os
import platform
import subprocess
import time
from pathlib import Path

out = Path("docs/results")
out.mkdir(parents=True, exist_ok=True)
compiler = os.environ.get("GO", "go")
started = time.time()
result = subprocess.run(
    ["make", "lint", "test", "bench"], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True
)
(out / "checks.log").write_text(result.stdout)
paths = [
    "internal/engine/engine.go",
    "internal/engine/views.go",
    "internal/worker/worker.go",
    "migrations/schema.sql",
    "cmd/bench/main.go",
]
metadata = {
    "command": "make lint test bench",
    "exit_code": result.returncode,
    "started_unix": started,
    "elapsed_seconds": time.time() - started,
    "platform": platform.platform(),
    "go": subprocess.check_output([compiler, "version"], text=True).strip(),
    "source_head": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
    "otel_export_enabled": bool(os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT")),
    "core_source_sha256": {p: hashlib.sha256(Path(p).read_bytes()).hexdigest() for p in paths},
    "database": "isolated local Postgres; connection secret omitted",
}
(out / "checks.json").write_text(json.dumps(metadata, indent=2) + "\n")
print(result.stdout)
raise SystemExit(result.returncode)
