# The dispatch office

## Direction

A yellow dispatch masthead, a dark departure-board readout and route-line step diagrams. The navigation becomes a horizontal office header. Dense workflow rows and explicit state labels replace soft dashboard cards.

## Outside reference

[SBB digital design guidance](https://digital.sbb.ch/en/), inspected on 2026-10-22. Design takeaway: Wayfinding should help an operator locate the next action and read status quickly.

This is an original composition and stylesheet. No reference-site code, logos, photographs or proprietary fonts were imported. Fonts use local system stacks; diagrams are original SVG/CSS. No new runtime dependency or third-party request was added.

## Data and behavior

The route illustration describes the existing validate/calculate/receipt pipeline. Workflow counts, retries and selection still come from the API. The public search filters actual saved reconciliation rows.

API contracts, authorization, persistence, core algorithms and benchmark datasets were not changed. Existing benchmark outputs remain historical. UI verification and new recordings are saved separately under [docs/results/ui-redesign](results/ui-redesign/).

## Interaction and access

Buttons retain their existing actions and accessible labels. Keyboard focus is visible. Layouts collapse for narrow screens; wide evidence tables scroll inside their own containers. Color is paired with text for status. Motion respects prefers-reduced-motion.

## Review artifacts

- [Actual console](results/ui-redesign/console-desktop.png)
- [Mobile console](results/ui-redesign/console-mobile.png)
- [Browser flow results](results/ui-redesign/browser.json)
- [New console recording](results/ui-redesign/demo.webm)
- [Recording probe](results/ui-redesign/demo-probe.json)
- [Frontend build log](results/ui-redesign/frontend-build.log)

The saved [review-console.py](results/ui-redesign/review-console.py) is the existing real-service browser flow adapted to write into the UI review directory. Run it only against an isolated local demo stack with the normal operator environment configured. Historical performance is not rebenchmarked by this visual review.

Public evidence preview: [desktop](results/ui-redesign/site-desktop.png), [mobile](results/ui-redesign/site-mobile.png), [checks](results/ui-redesign/site-review.json). Publication is a separate approval step.

## Verification disposition

Final build, lint and existing test suites pass. The redesigned console was exercised against actual isolated local services, not mocked counters. The public-page previews use saved evidence and the new actual recording. The local proof was captured before publication. The repository CI workflow reports the currently pushed revision; README lists the public URL where available.

The initial capture encoded 89.60 seconds and failed the minimum-duration gate; [the raw probe](results/ui-redesign/initial-demo-probe.json) is retained. A longer real capture passed with 97.96 seconds. No frames were padded or fabricated.

CI caught an incomplete hosted-image COPY manifest after the recording path changed. [The actual failing container build](results/ui-redesign/initial-hosted-image-failure.log) is retained. Dockerfile.hosted now supplies the new recording and poster explicitly; [the isolated copy-stage builder](results/ui-redesign/hosted-copy-build.log) passes. The unchanged CI container smoke gate verifies the real image and independent daemon.
