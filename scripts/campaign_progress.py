"""Read-only monitoring of a named benchmark schema; never mutates campaign state."""

import json
import os
import re
import subprocess
import sys
from pathlib import Path

schema = sys.argv[1]
if not re.fullmatch(r"bench_[0-9a-f]{32}", schema):
    raise SystemExit("Expected an isolated benchmark schema name")
query = f"""SET search_path TO {schema};
SELECT jsonb_build_object('observed_at',clock_timestamp(),
 'runs',(SELECT count(*) FROM runs),'done',(SELECT count(*) FROM runs WHERE state='done'),
 'pending',(SELECT count(*) FROM runs WHERE state='running'),'dead',(SELECT count(*) FROM runs WHERE state='dead'),
 'effects',(SELECT count(*) FROM effects),
 'missing_effects',(SELECT count(*) FROM steps s LEFT JOIN effects e USING(run_id,position) WHERE s.state='done' AND e.run_id IS NULL),
 'orphan_effects',(SELECT count(*) FROM effects e JOIN steps s USING(run_id,position) WHERE s.state<>'done'));
"""
raw = subprocess.check_output(
    ["psql", os.environ["TEST_DATABASE_URL"], "-At", "-c", query], text=True
)
row = json.loads(next(line for line in raw.splitlines() if line.startswith("{")))
path = Path("docs/results/hour-progress.jsonl")
with path.open("a") as output:
    output.write(json.dumps(row, sort_keys=True) + "\n")
print(json.dumps(row, indent=2))
