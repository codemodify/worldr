# Phase-two ten-item roadmap

This is the ten-item phase-two program approved on 2026-09-18. The status of
each item separates code that is integrated and tested from observations that
require particular hardware. The implementation slice for every item is
present; the remaining qualification boundaries are listed after the
milestones.

1. **Qualify the physical direct-display path — automation complete; hardware
   observation outstanding.** `scripts/try-tty.sh` performs a bounded
   `vk-display` or diagnostic DRM run from a spare TTY, records the system,
   connector list, logs, metrics, state and exit status, and leaves a concrete
   multi-monitor/input/VT/suspend checklist. It deliberately refuses to take
   over a display from an active X11 or Wayland session. A real spare-TTY run
   remains necessary because a nested or headless test cannot establish
   connector, VT, seat, suspend or hotplug behavior on physical hardware.

2. **Deliver a flagship native engineering and research workbench —
   implemented and tested.** CSV, TSV and explicit `.worldr-data.json` files
   open as linked tables, line/scatter charts and pickable 3D observations.
   Axis selection, inspection, orbit/zoom, forced reload, live file refresh,
   exact session restore, semantics, bounded parsing and resource retirement
   are integrated. See [RESEARCH.md](RESEARCH.md).

3. **Publish a stable native application SDK — v1 implemented and tested.**
   `sdk/nativeapp/v1` defines a versioned, bounded, length-framed process
   protocol for retained RGBA textures, meshes, spatial objects, materials,
   picking, lifecycle, input, IME contexts and semantic trees. The independent
   orbital-instrument example runs through `--native-app`; host and app both
   validate snapshots and resource lifetimes. See
   [NATIVE-DEVELOPMENT.md](NATIVE-DEVELOPMENT.md).

4. **Add spatial portals and navigation — implemented and
   tested.** Each named space and window group has a stable portal. Camera
   targets survive save/restore, portal travel is undoable, `Ctrl+Alt+G` opens
   the portal atlas, and `Ctrl+Alt+Left/Right` travels between groups. Portal
   discovery stays in those explicit controls rather than covering zoomed-out
   windows with title cards. Navigation never grants keyboard focus to an
   application.

5. **Raise cinematic rendering quality — bounded SDR slice implemented and
   tested.** Cinematic presentation adds moving point lights and thin-glass
   transmission plus opt-in, bounded refraction and frosted blur of an opaque
   scene prepass, plus depth-composited native holographic projections whose
   workspace phase freezes under Reduced Motion, while retaining directional
   shadows, depth-peeled transparency and depth-aware authored glow. Refraction
   and holograms are confined to explicit native mesh materials and exclude
   screen overlays and the host cursor; zero values preserve the
   prior transmission path and exact client/legacy texture pixels. The renderer
   also supplies opt-in exposure, saturation, contrast, filmic tone mapping and
   bounded highlight bloom for an experience that owns every final pixel. The
   mixed compositor workspace keeps that global transform neutral so legacy
   buffers, opaque UI and the host cursor retain exact colors and input bounds.
   Adaptive presentation fades the authored effects and Reduced Motion freezes
   ambient light and hologram movement. Physical output remains SDR sRGB; HDR signaling and
   ICC/wide-gamut transforms remain separate work. See
   [RENDERING.md](RENDERING.md).

6. **Broaden DMA-BUF and real application compatibility — implemented for the
   stated acceptance subset.** The retained GPU import path accepts supported
   single-plane explicit-modifier XRGB/ARGB/XBGR/ABGR buffers in 8888 and
   2101010 layouts with exact capability gating, including supported
   non-LINEAR input copied into owned LINEAR snapshots. Isolated real-client suites cover
   Chromium, Konsole, foot and xmessage behavior including scaling, popups,
   clipboard and Wayland drag-and-drop. Socket-level coverage also verifies
   acknowledge-gated XDG shell v3 explicit/reactive popup repositioning.
   Xwayland now selects glamor only when the renderer's exact DRM device and a
   common explicit XRGB/ARGB modifier are available, and safely retries with
   SHM. Managed X11 clients have bounded, copy-only XDND versions 3–5 with a
   real source-to-destination payload gate. Multi-plane and broader format and
   toolkit coverage, cross-protocol X11/Wayland drags and non-copy XDND actions
   remain expansion work.

7. **Build structured native terminal workflows — implemented and tested.**
   Search, bookmarks, OSC 133 command blocks, folds and pins now include a Tasks
   deck. A command can be captured as a bounded recipe, inspected, explicitly
   staged, run, copied or deleted with visible running/exit status. Workspace
   restore keeps recipes inert: it never replays a command or process. See
   [TERMINAL-WORKFLOWS.md](TERMINAL-WORKFLOWS.md).

8. **Add native notes, documents, dashboards and scientific visualization —
   implemented and tested.** The research workbench supplies the linked
   dashboard and 3D data view. Explicit `.worldr-note.md` files open in a
   bounded multiline editor with grapheme-safe selection, undo/redo, clipboard,
   IME, durable atomic saves, external-change detection, semantics and complete
   recovery of dirty text. Ordinary source and Markdown files remain read-only
   Files previews. See [NOTES.md](NOTES.md) and [RESEARCH.md](RESEARCH.md).

9. **Integrate accessibility, IME, packaging and login sessions — implemented
   to the documented boundary.** Native semantic trees are remapped to public
   window IDs and exported through a bounded private Unix JSON stream. Native
   editable fields use XKB and nested text-input-v3 preedit/commit contexts.
   `make install` stages `worldr-shell`, the supervising `worldr-session`, a
   display-manager Wayland session and a nested desktop entry. An OS AT-SPI
   desktop-bus adapter remains future interoperability work. See
   [ACCESSIBILITY.md](ACCESSIBILITY.md).

10. **Establish repeatable daily-use reliability trials — default trial
    implemented and passed.** `scripts/soak.sh` runs Files, a real PTY, an inert
    task recipe, the model inspector, live research data, a file-backed note, a
    dirty untitled note and the public SDK instrument across process restarts.
    It checks every state/metrics/snapshot artifact, exact restored identities,
    private state permissions and non-execution of the saved task. The default
    three-cycle, one-hour trial passed on Intel Graphics (ARL): 215,965 frames
    over 3,600.03 seconds, stable 188.3–192.1 MiB peak RSS, 98.1 MiB tracked
    Vulkan peak in every process and zero graphics recoveries. Continued human
    daily use remains ongoing product evidence rather than a bounded test. See
    [QUALIFICATION.md](QUALIFICATION.md) and [PERFORMANCE.md](PERFORMANCE.md).

## Remaining qualification and expansion

All ten implementation slices are integrated, and the default reliability trial
has passed. Completion does not turn bounded acceptance subsets into universal
platform support. The remaining external evidence is a physical spare-TTY
multi-monitor/VT/suspend run. Product expansion includes HDR/ICC/wide-gamut
output, an AT-SPI adapter, more DMA-BUF modifiers and formats, cross-protocol
X11/Wayland drag-and-drop,
broader toolkit coverage, larger/domain-specific research tools
and SDK services such as clipboard or brokered file access.

The automated suites remain part of every change: normal and no-CGO Go tests,
race checks, strict C warnings, Vulkan pixel tests, real toolkit/media tests,
state validation and script syntax checks. Hardware-dependent observations must
be recorded as observations rather than inferred from headless or nested tests.

The post-phase-two selection, including deliberately pinned work, is recorded in
[the product backlog selection](BACKLOG.md).
