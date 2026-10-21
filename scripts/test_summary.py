"""Persist and summarize actual Go test events, including a failure exit."""
import json
from pathlib import Path
import sys

source = Path(sys.argv[1])
rows = [json.loads(line) for line in source.read_text().splitlines() if line.strip()]
passed = [r for r in rows if r.get('Action') == 'pass' and 'Test' in r]
failed = [r for r in rows if r.get('Action') == 'fail']
Path('docs/results').mkdir(parents=True, exist_ok=True)
Path('docs/results/tests.jsonl').write_text(source.read_text())
summary = {'passed': len(passed), 'failed': len(failed),
           'tests': [r['Test'] for r in passed], 'failures': failed}
Path('docs/results/tests.json').write_text(json.dumps(summary, indent=2)+'\n')
print(json.dumps(summary, indent=2))
if failed or not passed:
    raise SystemExit(1)
