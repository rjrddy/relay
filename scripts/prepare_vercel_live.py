"""Provision private operator secrets; activate services after DATABASE_URL exists."""

import argparse
import secrets
import subprocess
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--enable", action="store_true")
args = parser.parse_args()
cli = ["npx", "--yes", "vercel@65.0.0"]
listing = subprocess.run(
    cli + ["env", "ls", "--environment", "production"],
    capture_output=True,
    text=True,
    check=True,
    timeout=60,
)
names = listing.stdout + listing.stderr
path = Path(".env.vercel-operator")
if not path.exists():
    path.write_text(
        "RELAY_PASSWORD="
        + secrets.token_urlsafe(24)
        + "\nRELAY_API_TOKEN="
        + secrets.token_urlsafe(32)
        + "\n"
    )
    path.chmod(0o600)
values = dict(line.split("=", 1) for line in path.read_text().splitlines() if "=" in line)
for name, value in {**values, "RELAY_SEED": "1"}.items():
    if name not in names:
        result = subprocess.run(
            cli + ["env", "add", name, "production"],
            input=value + "\n",
            capture_output=True,
            text=True,
            timeout=60,
        )
        if result.returncode:
            raise SystemExit("Could not provision " + name + "; inspect Vercel settings.")
        print("Provisioned " + name + " without displaying its value.")
print("Operator credentials are in ignored .env.vercel-operator; do not commit it.")
if args.enable:
    if "DATABASE_URL" not in names:
        raise SystemExit("Configure production DATABASE_URL before enabling live services.")
    Path("vercel.json").write_text(Path("deploy/vercel-live.json").read_text())
    print("Live config enabled. Deploy: npx vercel@65.0.0 deploy --prod --yes")
else:
    print("Evidence hosting remains active. Use --enable after configuring DATABASE_URL.")
