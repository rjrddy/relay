"""Run the actual hosted API and daemon binary against an isolated synthetic database."""

import json
import os
import subprocess
import sys
import time
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "sdk/python"))
from relay_sdk import Client

env = {
    **os.environ,
    "DATABASE_URL": os.environ["TEST_DATABASE_URL"],
    "RELAY_PASSWORD": "synthetic-hosted-smoke-password",
    "RELAY_API_TOKEN": "synthetic-hosted-smoke-token-only-123456",
    "RELAY_WEB": "apps/web/dist",
    "RELAY_PROOF": "deploy/showcase/dist",
    "PORT": "18083",
}
log = Path("docs/results/hosted-native.log").open("wb")
processes = [
    subprocess.Popen(["bin/relay-hosted", *args], env=env, stdout=log, stderr=subprocess.STDOUT)
    for args in ([], ["worker"])
]
try:
    for _ in range(100):
        try:
            urllib.request.urlopen("http://127.0.0.1:18083/health", timeout=1).close()
            break
        except OSError:
            time.sleep(0.1)
    else:
        raise AssertionError("hosted health failed")
    client = Client("http://127.0.0.1:18083", env["RELAY_API_TOKEN"])
    key = "hosted-smoke-" + str(time.time_ns())
    first = client.enqueue(key, 42, work_ms=50)
    duplicate = client.enqueue(key, 42, work_ms=50)
    assert first["id"] == duplicate["id"] and not duplicate["created"]
    for _ in range(200):
        detail = client.run(first["id"])
        if detail["run"]["state"] == "done":
            break
        time.sleep(0.05)
    assert detail["run"]["state"] == "done"
    assert len(detail["effects"]) == 3 and detail["effects"][-1]["result"] == 84
    assert all(e["trace_id"] != "0" * 32 for e in detail["events"])
    for route, marker in [("/", "Workers can fail"), ("/console/", '<div id="root">')]:
        with urllib.request.urlopen("http://127.0.0.1:18083" + route) as response:
            assert marker in response.read().decode()
    Path("docs/results/hosted-native.json").write_text(
        json.dumps(
            {
                "health": "pass",
                "isolated_schema": "relay_vercel_v1",
                "api_and_independent_worker": True,
                "duplicate_created": duplicate["created"],
                "effects": len(detail["effects"]),
                "receipt": detail["effects"][-1]["result"],
                "trace_ids_nonzero": True,
                "evidence_and_console_routes": True,
                "run": first["id"],
            },
            indent=2,
        )
        + "\n"
    )
    print("Actual hosted entrypoint API/worker/trace/routing smoke passed.")
finally:
    for process in processes:
        process.terminate()
        process.wait(timeout=10)
    log.close()
