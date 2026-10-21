"""Standard-library SDK with bounded, idempotent enqueue retries."""

import hashlib
import http.client
import json
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass


@dataclass
class APIError(Exception):
    status: int
    message: str

    def __str__(self) -> str:
        return f"Relay HTTP {self.status}: {self.message}"


class Client:
    def __init__(self, base_url: str, token: str, timeout: float = 10) -> None:
        self.base_url = base_url.rstrip("/")
        self.token, self.timeout = token, timeout

    def _call(self, method: str, path: str, payload: dict | None = None) -> dict:
        body = json.dumps(payload).encode() if payload is not None else None
        request = urllib.request.Request(
            self.base_url + path,
            data=body,
            method=method,
            headers={"Authorization": f"Bearer {self.token}", "Content-Type": "application/json"},
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            message = json.load(error).get("error", "request failed")
            raise APIError(error.code, message) from None

    def enqueue(
        self,
        key: str,
        value: int,
        *,
        delay_ms: int = 0,
        fail_until: int = 0,
        max_attempts: int = 3,
        work_ms: int = 0,
    ) -> dict:
        payload = {
            "key": key,
            "input": {
                "value": value,
                "fail_until": fail_until,
                "permanent_failure": False,
                "work_ms": work_ms,
            },
            "delay_ms": delay_ms,
            "max_attempts": max_attempts,
        }
        for attempt in range(5):
            try:
                return self._call("POST", "/api/runs", payload)
            except APIError as error:
                if error.status not in (429, 502, 503, 504) or attempt == 4:
                    raise
            except (
                urllib.error.URLError,
                TimeoutError,
                ConnectionError,
                http.client.IncompleteRead,
                json.JSONDecodeError,
            ):
                if attempt == 4:
                    raise
            jitter = int.from_bytes(hashlib.sha256(key.encode()).digest()[:2], "big") % 41
            time.sleep((100 * 2**attempt + jitter) / 1000)
        raise RuntimeError("unreachable retry state")

    def run(self, run_id: str) -> dict:
        return self._call("GET", "/api/runs/" + urllib.parse.quote(run_id, safe=""))
