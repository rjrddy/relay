"""Generate ignored local-only demo secrets without overwriting existing configuration."""

import os
import secrets
from pathlib import Path

path = Path(".env")
if path.exists():
    raise SystemExit(".env already exists; edit it explicitly instead of overwriting it")
password = secrets.token_hex(24)
descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(descriptor, "w") as output:
    output.write(
        f"POSTGRES_PASSWORD={secrets.token_hex(24)}\nRELAY_PASSWORD={password}\n"
        f"RELAY_API_TOKEN={secrets.token_hex(32)}\nCOOKIE_SECURE=0\n"
    )
print(
    "Created .env. Open .env locally to copy your generated operator password. Secrets are not printed."
)
