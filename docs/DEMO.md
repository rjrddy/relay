# Demo and recording

From a clone, run `python3 scripts/configure.py`, inspect the ignored .env locally for your generated operator password, then `docker compose up --build`. Open http://localhost:8080 and sign in. Do not publish .env or paste its secrets into recorded terminal output.

Operator flow:

- Enqueue a workflow and inspect the validate → calculate → receipt steps. Show committed effects, attempts and lease epochs.
- Pause dispatch. Enqueue another run while paused and show it waiting; resume and watch it advance.
- Select “Exhaust budget → dead letter”, enqueue, and inspect the failed calculate step. Replay the failed step. The completed validate effect and its original commit timestamp stay intact.
- Publish a partial rollout, then restore the previous settings. Show that every publication creates a version rather than erasing history.
- Admit a synthetic burst, open the execution timeline and inspect trace-linked events and the queue-latency distribution.

The automated `scripts/browser_check.py` records the actual browser flow, including a deliberately lost enqueue response after a real admission, followed by a page reload and a retry with the same immutable key. It also verifies mobile layout, no page errors and unchanged completed effects during replay. The resulting demo.webm is a silent screen recording, not a narrated presentation. ffprobe output in docs/results/demo-probe.json records the actual media duration.

To reproduce the automated recording, install the Python dev extras, install Playwright's Chromium and ffmpeg binaries, set RELAY_PASSWORD to the local operator password, and run `.venv/bin/python scripts/browser_check.py` against the running app. Set CHROMIUM_PATH only when using an existing browser executable. Browser binaries are development tools, not runtime dependencies of the service.

No hosted demo is deployed. deploy/render.yaml is preparation only; review host access, database lifetime and the optional embedded-worker mode before deploying.
