# Project-specific interface: The dispatch office

Status: accepted for local implementation; publication pending approval.

## Context

The existing portfolio consoles shared a generic dashboard vocabulary. This made unrelated engineering problems look alike and hid the project’s primary object. The user explicitly authorized a visual redesign of the five kept projects.

## Decision

A yellow dispatch masthead, a dark departure-board readout and route-line step diagrams. The navigation becomes a horizontal office header. Dense workflow rows and explicit state labels replace soft dashboard cards.

Research reference: [SBB digital design guidance](https://digital.sbb.ch/en/). Wayfinding should help an operator locate the next action and read status quickly.

The route illustration describes the existing validate/calculate/receipt pipeline. Workflow counts, retries and selection still come from the API. The public search filters actual saved reconciliation rows.

## Consequences

Maintain an independent visual vocabulary for this project; do not extract a shared cross-repository dashboard template. Preserve action names and API behavior so existing integration flows remain meaningful. Keep measured evidence and explicit hosting limits visible on the public page. Use system fonts and original code-native diagrams to keep the runtime self-contained. Save browser proof separately from the original benchmarks.

See [UI design and verification](../UI.md).
