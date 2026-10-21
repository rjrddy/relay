"""Nonmutating Go formatting gate, using the selected compiler's formatter."""

import subprocess
import sys
from pathlib import Path

compiler = sys.argv[1]
root = subprocess.check_output([compiler, "env", "GOROOT"], text=True).strip()
files = subprocess.check_output(
    ["git", "ls-files", "--cached", "--others", "--exclude-standard", "*.go"], text=True
).splitlines()
result = subprocess.run(
    [str(Path(root) / "bin/gofmt"), "-l", *files], capture_output=True, text=True
)
print(result.stdout, end="")
if result.returncode or result.stdout.strip():
    raise SystemExit(1)
