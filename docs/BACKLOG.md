# Product backlog selection

This file records the acceptance slices selected on 2026-09-18 after the
phase-two ten-item roadmap was integrated. `docs/ROADMAP.md` remains the evidence
record for that completed program; this document records the completed selection
and the work deliberately kept pinned.

## Completed program

The following workstreams have completed their documented acceptance slices,
tests and user-facing documentation. Broad platform claims still require the
relevant real-client or hardware evidence.

| ID | Workstream | State |
| ---: | --- | --- |
| 2 | Broader Wayland application compatibility | complete for this acceptance slice: XDG shell v3 explicit/reactive popup repositioning, commit-atomic incremental SHM damage, and isolated real Chromium/Konsole/foot coverage are integrated; broader toolkit acceptance remains expansion work |
| 3 | Complete X11 interoperability | complete for this acceptance slice: bounded lazy `CLIPBOARD`, capability-gated glamor DMA-BUF buffers with a safe SHM retry, and managed X11-to-X11 XDND v3–5 copy routing have protocol and real-client coverage; primary selection, cross-protocol drags and non-copy XDND actions remain outside the slice |
| 4 | Expand GPU buffer support | complete for this acceptance slice: capability-gated single-plane explicit DRM modifiers plus LINEAR 8888/2101010 RGB import, immutable LINEAR snapshots, linux-dmabuf v4 sealed exact-pair tables and exact-device feedback, and real GBM/Vulkan/protocol coverage are integrated; multi-plane YUV and explicit sync remain |
| 10 | Complete the cinematic rendering system | complete for this acceptance slice: bounded thin-glass refraction/frost and a depth-composited holographic projection material are integrated, including Reduced Motion phase freezing and exact overlay/client isolation; physical HDR/ICC, volumetric scattering and the broader concept-art finish remain |
| 12 | Unify experiences and native 3D applications | complete for this acceptance slice: AXIAL hosted app, launcher, exact session restore, spatial input routing and close/reopen GPU geometry retirement are integrated; additional top-level experiences and broader native-app services remain outside this slice |
| 14 | Renderer and session failure isolation | complete for this acceptance slice: connector-scoped device recovery plus PID-pinned, parent-death-tested and bounded login-session supervision are integrated; physical failure qualification and recovery from a wedged kernel remain outside this slice |
| 15 | Continuous compatibility and reliability evidence | complete for this acceptance slice: race coverage, validated nested recovery artifacts and an exact-checkpoint short soak are integrated into CI; physical-display and multi-day human evidence remain outside this slice |

Item 12's current slice is exercised by
`TestHostedAxialIsAReusableSpatialApplication` (launch, close, exact texture and
mesh retirement, fresh reopen identity),
`TestHostedAxialSpatialPickRoutesThroughWorkspace` (scene depth pick, capture,
selection and orbit through the generic application route), and
`TestHostedAxialLaunchPlacementAndSessionRestore` (hub identity, saved placement
and manifest round trip).

Item 14's session slice is exercised by deterministic process tests for graceful
signal forwarding, ignored-signal timeout, immediate second-signal escalation,
leader-first exit with a stubborn descendant, exact leader reaping, and Linux
parent-death delivery. The leader remains unreaped until the owned process group
has received final cleanup, preventing PID/PGID reuse during escalation.

## Pinned

Pinned work remains part of the product backlog but is deliberately deferred.
Do not pull it into another workstream merely because the same subsystem is
being edited.

| ID | Workstream |
| ---: | --- |
| 1 | Physical direct-session qualification |
| 5 | OS accessibility integration |
| 6 | International and direct-session input |
| 7 | Files and document authoring |
| 8 | Terminal text quality |
| 9 | Media pipeline expansion |
| 11 | Production native-app platform |
| 13 | More native engineering and research applications |

## Completion rules

- Compatibility support needs an isolated protocol test and, where a real
  client exists in the project matrix, a gated real-client acceptance test.
- GPU sharing needs explicit capability advertisement, strict descriptor and
  ownership validation, rendering verification, and a safe fallback.
- Cinematic rendering changes must preserve exact legacy application pixels in
  the mixed workspace unless an application explicitly opts into an effect.
- Experience migration must preserve saved workspace identity and must not
  execute restored processes or commands implicitly.
- Failure isolation must keep state saving independent from renderer recovery
  and place a finite bound around the supervising session's shutdown path.
- Reliability claims must name the actual environment and must not infer
  physical-display results from nested or headless tests.
