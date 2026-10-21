"""Real HTTP/Python-SDK proof against a running native or Compose stack."""
import json
import os
from pathlib import Path
import sys
import time
import urllib.error
import urllib.request
from uuid import uuid4
sys.path.insert(0, str(Path(__file__).resolve().parents[1]/'sdk/python'))
from relay_sdk import APIError, Client

base = os.environ.get('RELAY_URL', 'http://127.0.0.1:8080')
client = Client(base, os.environ['RELAY_API_TOKEN'])
key = 'smoke-'+uuid4().hex
first = client.enqueue(key, 21, fail_until=1, work_ms=10)
second = client.enqueue(key, 21, fail_until=1, work_ms=10)
assert first['id'] == second['id'] and first['created'] and not second['created']
try:
    client.enqueue(key, 999, fail_until=1, work_ms=10)
except APIError as error:
    assert error.status == 409
else:
    raise AssertionError('idempotency conflict not rejected')
started=time.monotonic()
while True:
    detail=client.run(first['id'])
    if detail['run']['state']=='done': break
    assert detail['run']['state']!='dead'
    if time.monotonic()-started > 60: raise AssertionError('completion deadline')
    time.sleep(.1)
assert len(detail['effects'])==3 and detail['effects'][-1]['result']==42
assert all(e['trace_id']!='0'*32 for e in detail['events'])
with urllib.request.urlopen(base+'/') as response:
    html=response.read().decode()
    assert '<div id="root">' in html
result={'sdk_idempotency':True,'conflict_status':409,'run':first['id'],'completed_steps':len(detail['effects']),
        'receipt':detail['effects'][-1]['result'],'event_trace_ids_nonzero':True,'elapsed_seconds':time.monotonic()-started}
Path('docs/results').mkdir(parents=True,exist_ok=True)
Path(os.environ.get('SMOKE_OUTPUT','docs/results/http-smoke.json')).write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result,indent=2))
