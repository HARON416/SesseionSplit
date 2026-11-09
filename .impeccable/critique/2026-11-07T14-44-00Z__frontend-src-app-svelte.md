---
target: SessionSplit cards and overall UI
total_score: 26
max_score: 40
na_heuristics: 
p0_count: 0
p1_count: 0
target_identity: "file:/home/kibet/Projects/SessionSplit/frontend/src/App.svelte"
target_fingerprint: "sha256:16f50bb842ca5e51dc44ed34effd8d64a6776a6bd3504b845464d7d7a8a47ddb"
target_path: /home/kibet/Projects/SessionSplit/frontend/src/App.svelte
timestamp: 2026-11-07T14-44-00Z
slug: frontend-src-app-svelte
closed: true
---
SessionSplit cards and overall UI — independent dual-assessment critique

Target: frontend/src/App.svelte and frontend/src components. Mode: Operate.

Design specificity: coherent Postika-derived desktop utility. Inter, square controls, uppercase names, neutral Open actions, and consistent themes form a deliberate system. Familiar rather than distinctive; appropriate to this task. Refine the composition rather than replacing it.

| Nielsen heuristic | Score /4 |
|---|---:|
| Visibility of system status | 3 |
| Match with real world | 4 |
| User control and freedom | 3 |
| Consistency and standards | 3 |
| Error prevention | 3 |
| Recognition rather than recall | 3 |
| Flexibility and efficiency | 1 |
| Aesthetic and minimalist design | 3 |
| Error recovery | 2 |
| Help and documentation | 1 |
| Total | 26/40 |

Rating: Acceptable. Subjective usability heuristic assessment, not an automated visual-quality score. All ten heuristics apply.

Strengths: Profile names and Open buttons establish a clear hierarchy. Long names wrap within short wide cards at desktop and narrow widths. Both themes and the custom scrollbar are coherent. Busy states, confirmation, persistence, and focus restoration show care.

Priority issues:
1. P2 — Backend diagnostics flow directly into errors through Error.message in App.svelte lines 62, 77, 104; profiles.go includes create profile storage, launch browser, connect to browser messages. User confusion is inferred from the production error paths; fixture injects friendlier errors. Map expected failures to actionable plain-language messages while preserving retry and entered values. Route: impeccable harden.
2. P2 — Metadata labels use 11px and values 12px (App.svelte lines 236–239). Trial 12px labels and 13px values, preserving compact card geometry and retesting narrow layouts. Legibility concern, not a measured contrast failure. Route: impeccable typeset.
3. P2 — Delete precedes Open in DOM/tab order (App.svelte lines 166–167). Make Open first in DOM and visual order. Add a discoverable Add accelerator only if frequent-use needs justify it. Route: impeccable adapt.

Cognitive load: low in the three-profile fixture, no clear checklist failures. Choices are grouped by profile; do not treat every visible button as one unstructured decision. Large-profile retrieval was not tested and search is not assumed necessary.

Emotional journey: creation ends with new-card Open focus; deletion explains browser closure and permanent data/download removal and initially focuses Cancel. Production launch/storage failure is the inferred emotional valley because recovery instructions may be missing.

Personas: Alex (frequent user) encounters Delete before Open and lacks accelerators. Sam (keyboard/low vision) encounters small metadata and action-order friction, with a sound semantic foundation. Jordan (first timer) has a clear Add action but little explanation of profile isolation. No audience-specific personas invented.

Minor observations: grid rows stretch short-name cards alongside long names; Chrome icon and badge repeat identification; Close versus Cancel differs between dialogs. Not recorded is accurate for legacy history, so do not replace universally with Never opened.

Deterministic scan: impeccable detect --json frontend/src exited 0, returned [], zero findings and no false positives. Does not establish usability or accessibility compliance.

Browser evidence: TestProfilesUI passed in 5.713s. Verified create/persistence/duplicates, correct Open and timestamps, failed Open retaining timestamps, delete Cancel/failure/retry/persistence, empty-state focus; long names contained at 1200x800 and 480x800 in both themes. At 480x320, actual overflow, 8px scrollbar, and wheel movement verified. Fresh screenshot artifacts in /tmp/sessionsplit-critique-browser/. Chrome/Rod fixture is fallback evidence, not native WebKit execution.

Limitations: native WebKitGTK has no CDP; no native detector attachment or human overlay. Complete keyboard-only flow, screen readers, measured contrast, 200% zoom, high-contrast OS mode and visible-focus rendering were not validated.

Questions to guide next work:
- First priority: card readability, keyboard flow, or error recovery?
- Scope: the three priority fixes, or those plus brief first-use guidance and minor copy consistency?
