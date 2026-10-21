"""Exercise and record a real ninety-second operator flow; no mocked UI counters."""

import json
import os
import time
from pathlib import Path

from playwright.sync_api import sync_playwright

out = Path("docs/results")
out.mkdir(exist_ok=True, parents=True)
started = time.monotonic()
with sync_playwright() as p:
    browser = p.chromium.launch(executable_path=os.environ.get("CHROMIUM_PATH"))
    context = browser.new_context(
        viewport={"width": 1440, "height": 1000},
        record_video_dir=str(out / "demo"),
        record_video_size={"width": 1440, "height": 1000},
    )
    page = context.new_page()
    errors = []
    page.on("pageerror", lambda e: errors.append(str(e)))
    page.goto(os.environ.get("RELAY_URL", "http://127.0.0.1:8080"))
    page.get_by_label("Operator password").fill(os.environ["RELAY_PASSWORD"])
    page.get_by_role("button", name="Enter workspace").click()
    page.get_by_role("heading", name="Every step accounted for.").wait_for()
    page.evaluate(
        """async()=>{const session=await(await fetch('/api/session')).json();const c=await(await fetch('/api/control')).json();await fetch('/api/control',{method:'PUT',headers:{'Content-Type':'application/json','X-CSRF-Token':session.csrf},body:JSON.stringify({...c,paused:false,rollout:100})});}"""
    )
    page.reload()
    page.get_by_role("heading", name="Every step accounted for.").wait_for()
    ambiguous = {}

    def lose_ack(route):
        if route.request.method != "POST":
            route.continue_()
            return
        response = route.fetch()
        ambiguous["id"] = response.json()["id"]
        ambiguous["key"] = route.request.post_data_json["key"]
        route.fulfill(status=503, json={"error": "injected acknowledgement loss after admission"})

    page.route("**/api/runs", lose_ack)
    page.get_by_role("button", name="Enqueue workflow", exact=False).click()
    page.get_by_role("button", name="Retry pending workflow", exact=False).wait_for()
    page.get_by_role("alert").filter(has_text="injected acknowledgement loss").wait_for()
    page.unroute("**/api/runs", lose_ack)
    page.reload()
    with page.expect_response(
        lambda r: r.url.endswith("/api/runs") and r.request.method == "POST"
    ) as retry:
        page.get_by_role("button", name="Retry pending workflow", exact=False).click()
    acknowledged = retry.value.json()
    assert acknowledged["id"] == ambiguous["id"] and not acknowledged["created"]
    observed = page.evaluate("async()=> (await fetch('/api/runs')).json()")
    assert sum(r["idem_key"] == ambiguous["key"] for r in observed) == 1

    def wait_state(run_id, state):
        deadline = time.monotonic() + 60
        while True:
            detail = page.evaluate("async(id)=>(await fetch('/api/runs/'+id)).json()", run_id)
            if detail.get("run", {}).get("state") == state:
                return detail
            if time.monotonic() > deadline:
                raise AssertionError(f"Workflow did not reach {state}: {detail}")
            page.wait_for_timeout(250)

    page.get_by_role("button", name="Pause dispatch", exact=True).click()
    page.get_by_text("Dispatch paused", exact=True).wait_for()
    page.get_by_label("Failure injection").select_option("2")
    with page.expect_response(
        lambda r: r.url.endswith("/api/runs") and r.request.method == "POST"
    ) as response:
        page.get_by_role("button", name="Enqueue workflow", exact=False).click()
    run = response.value.json()["id"]
    page.get_by_role("button", name="Resume dispatch", exact=True).click()
    before_replay = wait_state(run, "dead")
    (out / "browser-before-replay.json").write_text(json.dumps(before_replay, indent=2) + "\n")
    prefix = before_replay["effects"]
    assert len(prefix) == 1 and prefix[0]["position"] == 0, repr(prefix)
    replay = page.get_by_role("button", name="Replay failed step", exact=True)
    replay.wait_for()
    page.wait_for_timeout(1600)
    replay.click()
    wait_state(run, "done")
    page.locator("#rollout").focus()
    page.keyboard.press("Home")
    for _ in range(50):
        page.keyboard.press("ArrowRight")
    page.get_by_role("button", name="Publish rollout", exact=True).click()
    page.wait_for_timeout(1600)
    control = page.evaluate("async()=> (await fetch('/api/control')).json()")
    assert control["rollout"] == 50
    page.get_by_role("button", name="Rollback previous", exact=False).click()
    deadline = time.monotonic() + 15
    while page.evaluate("async()=> (await fetch('/api/control')).json()")["rollout"] != 100:
        assert time.monotonic() < deadline, "rollout rollback deadline"
        page.wait_for_timeout(250)
    page.get_by_role("button", name="Admit demo burst", exact=True).click()
    page.wait_for_timeout(700)
    page.evaluate("scrollTo(0,0)")
    page.screenshot(path=str(out / "console-desktop.png"), full_page=True)
    desktop_overflow = page.evaluate("document.documentElement.scrollWidth>innerWidth")
    page.set_viewport_size({"width": 390, "height": 844})
    page.evaluate("scrollTo(0,0)")
    page.screenshot(path=str(out / "console-mobile.png"), full_page=True)
    mobile_overflow = page.evaluate("document.documentElement.scrollWidth>innerWidth")
    page.set_viewport_size({"width": 1440, "height": 1000})
    page.locator("#detail").scroll_into_view_if_needed()
    page.wait_for_timeout(4000)
    page.locator("#controls").scroll_into_view_if_needed()
    page.wait_for_timeout(4000)
    page.get_by_role("button", name="Admit demo burst", exact=True).click()
    page.wait_for_timeout(6000)
    page.locator("#runs").scroll_into_view_if_needed()
    page.wait_for_timeout(5000)
    page.evaluate("scrollTo(0,0)")
    while time.monotonic() - started < 90:
        page.wait_for_timeout(1000)
    detail = page.evaluate("""async(id)=>(await fetch('/api/runs/'+id)).json()""", run)
    assert detail["run"]["state"] == "done" and len(detail["effects"]) == 3
    assert detail["effects"][0] == prefix[0]
    assert not errors and not desktop_overflow and not mobile_overflow
    result = {
        "run": run,
        "actions": [
            "login",
            "lost admission acknowledgement recovered after reload",
            "pause",
            "enqueue while paused",
            "resume",
            "dead letter",
            "replay failed step",
            "completed prefix preserved",
            "publish rollout",
            "rollback",
            "demo burst",
            "trace-linked timeline",
            "desktop and mobile layout",
        ],
        "page_errors": errors,
        "desktop_overflow": desktop_overflow,
        "mobile_overflow": mobile_overflow,
        "final_receipt": detail["effects"][-1]["result"],
        "completed_prefix_preserved": True,
        "ambiguous_admission_deduplicated": True,
        "effects": len(detail["effects"]),
        "elapsed_seconds": time.monotonic() - started,
    }
    video = page.video
    context.close()
    video.save_as(str(out / "demo.webm"))
    (out / "browser.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))
    browser.close()
