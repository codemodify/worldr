# Renderer performance observations

## Hosted-AXIAL restart gate, 2026-09-18

After AXIAL moved into the general application contract, the current CI-sized
soak completed two five-second headless lifetimes at 1280x800 and 60 fps on
Intel Graphics (ARL). Both processes restored Files, a native PTY and inert
task, model and research tools, hosted AXIAL, two native notes, and the public
SDK instrument with stable identities and no restore error, SDK loss, task
replay, or graphics recovery. The artifact-audited rerun rendered 258 and 293
frames; it preserved the complete paused AXIAL record and, after assigning the
two newly seeded note slots, the complete workspace/session checkpoint exactly.
Its tracked Vulkan allocation peaked at 142.3 MB. The evidence is in
`dist/ci-soak-audit-2`; this short run is a correctness gate rather than a
performance baseline.

This short gate validates the expanded restart path and its assertions. The
one-hour observation below predates hosted AXIAL and remains the sustained
measurement for its stated workload; it does not retroactively measure the new
3D application.

## Expanded one-hour restart/resume trial, 2026-09-18

The expanded `scripts/soak.sh` workload completed its default three consecutive
20-minute headless lifetimes at 1280×800 and 60 fps on Intel Graphics (ARL).
Every process hosted Files, a real native PTY, the OBJ model inspector, the live
CSV research workbench, one file-backed note, one dirty untitled note and the
out-of-process SDK instrument. The terminal session also retained a task recipe
whose unique sentinel remained absent, proving that restoration did not execute
the saved command. Each restart validated exact resource/layout identities,
note text and disk identity, the terminal directory/task, private state mode,
SDK negotiation, metrics, logs and a rendered PNG. The dataset grew between
cycles and the research view refreshed from four to six visible observations.

The three processes rendered **215,965 frames in 3,600.03 seconds**. No graphics
recovery, metrics error, restore error, SDK loss or task replay occurred.

| Cycle | Frames | Work mean / p95 / p99 | Render mean / p95 | Frame interval p99 | Peak RSS | Go heap peak | Vulkan peak |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 71,990 | 8.88 / 11.3 / 15.4 ms | 8.56 / 10.7 ms | 21.5 ms | 188.3 MiB | 95.4 MiB | 98.1 MiB |
| 2 | 71,990 | 9.01 / 11.5 / 14.8 ms | 8.67 / 10.9 ms | 21.0 ms | 192.1 MiB | 95.4 MiB | 98.1 MiB |
| 3 | 71,985 | 8.86 / 11.3 / 14.0 ms | 8.54 / 10.7 ms | 20.5 ms | 191.7 MiB | 95.4 MiB | 98.1 MiB |

The local evidence is `/tmp/worldr-soak-phase2-full-20260918`. It includes the
three metrics files, logs, final captures, fixtures, SDK executable and final
workspace document. The run had no scripted input and does not establish
physical input-to-photon latency, direct-display behavior or multi-day human
usage. Those boundaries remain separate from this restart/resource trial.

## Phase-two restart/resume trial, 2026-09-18

The initial `scripts/soak.sh` path completed two consecutive 60-second headless
runs at 1280×800 and 60 fps on Intel Graphics (ARL). Each process hosted Files,
a real native PTY, the OBJ model inspector and the live CSV research workbench.
The first run created the workspace; the second reopened all four native windows
from the same document with a fresh shell. The script changed the CSV between
runs, verified every metrics/snapshot/state artifact, rejected metrics errors or
zero frames, and confirmed that terminal/model/research stable associations
survived the restart.

Both runs rendered exactly 3,600 frames in 60.01 seconds. Whole-frame CPU work
averaged 7.91 and 8.07 ms; p95 was 10.3 and 11.6 ms. Render submission/wait
averaged 7.70 and 7.85 ms; p95 was 9.8 and 11.4 ms. The frame-interval p99 was
21.1 and 22.1 ms. Peak process RSS was 154.6 and 155.7 MiB, and tracked Vulkan
allocation remained 94.3 MiB in both processes. No graphics recovery or metrics
error occurred. This tests process restart and native resource restoration; it
does not replace the longer mixed-media result below or physical input/display
qualification.

The reusable command defaults to three 20-minute cycles. The current harness
also relaunches the out-of-process SDK instrument, restores a file-backed note
and an exact dirty untitled note, and preserves an inert terminal task recipe.
It checks that the recipe never executes during restore. Those additions were
made after the two 60-second observations above and do not retroactively expand
that measurement's workload:

```sh
make test-soak
```

See [the reliability and physical qualification guide](QUALIFICATION.md) for
artifact validation, shorter overrides and the separate spare-TTY procedure.

## Sustained mixed workspace, 2026-09-18

A 60.01-second **1440×900, 4× MSAA** headless run on Intel Graphics (ARL)
rendered **3,592 frames** (59.86 fps) while hosting Files, a native PTY terminal,
an OBJ model inspector, two photos, two continuously playing videos, and a foot
terminal printing ten lines per second. Both videos used an authored 192-second
loop fixture; saved playback positions advanced to 59.8 and 79.73 seconds and
both remained unpaused. This avoids counting ended video players as active media.
Concurrent CPU build/test activity was not controlled; no other GPU test ran.

| CPU wall-clock measurement | Mean | p95 | p99 | Maximum |
| --- | ---: | ---: | ---: | ---: |
| Whole frame work, excluding pacing | 10.27 ms | 15.4 ms | 18.3 ms | 64.15 ms |
| Render submission and wait | 8.88 ms | 12.1 ms | 15.1 ms | 48.92 ms |
| Interval between rendered frames | 16.69 ms | 22.7 ms | 25.3 ms | 30.95 ms |

Peak process RSS was **322.5 MiB**, sampled Go heap **122.6 MiB**, and tracked
Vulkan allocation peak **122.4 MiB**. RSS excludes child programs; Vulkan counts
exclude driver/swapchain storage. Snapshot export is excluded from timings.
This run contained no input events or forced recovery, so it makes no input
latency or recovery claim. It demonstrates one mixed workload on one GPU,
including occasional missed 60 Hz frame deadlines; it is not a hardware-wide
frame-rate guarantee.

The local artifacts are `dist/mixed-benchmark-active-60s.json`, its matching log,
and `dist/worldr-mixed-workspace.png`. The task-specific saved manifest and
launch profile contain local fixture paths. After generating those fixtures:

```sh
./bin/worldr-shell --backend=headless --duration=60s \
  --state=dist/mixed-benchmark-state.json \
  --apps=dist/mixed-benchmark-apps.json \
  --metrics=dist/mixed-benchmark-active-60s.json \
  --snapshot=dist/worldr-mixed-workspace.png
```

`--metrics=PATH` writes bounded whole-run histograms (0.1 ms percentile bins),
input-dispatch-to-render-return time, process memory, and owned GPU allocations.
This is CPU dispatch latency, not hardware-event-to-photon latency. A configurable
`--gpu-memory-mib` limit bounds owned Vulkan allocations; device-loss recovery is
limited to two attempts per minute. The earlier `mixed-benchmark-60s.json` run
included videos reaching their ends, and is not the continuous-media baseline.

## Isolated nested input and recovery, 2026-09-18

An isolated test ran Worldr as a real Wayland/Vulkan client of a private
in-process compositor. It hosted a native PTY, photo and OBJ inspector, then
sent pointer and keyboard events over the Wayland connection. The private
socket ensures those scripted events cannot reach the user's desktop.

The run handled **131 input events** over **123 rendered frames**. Of 108
dispatch-to-render-return samples, the mean was **6.96 ms**, p95 was **9.7 ms**,
p99 was **13.9 ms**, and the maximum was **64.47 ms**. This remains a CPU-side
dispatch measurement rather than hardware-event-to-photon latency.

During the same run, the test forced both a resize allocation failure and a
complete Vulkan device recreation. It verified the restored pixels, terminal
focus and typed PTY content after two graphics recoveries. Peak tracked Vulkan
allocation was **83.5 MiB** and peak retained GPU snapshot storage was
**3.9 MiB**. The artifacts are `dist/nested-input-recovery.json` and
`dist/nested-input-recovery.png`.

```sh
WORLDR_TEST_NESTED=1 go test -v ./internal/app \
  -run TestIsolatedNestedWorkspaceInputAndRecoveryGPU
```

## Historical short observations

On **2026-09-17**, short runs on **Intel(R) Graphics (ARL)** showed lower CPU
submit/wait time after replacing AXIAL's ten overlapping halo discs with one
radial-gradient triangle fan. Both versions used **4× MSAA** at **2880×1800**.
These are three-second smoke observations on one machine, not a sustained
benchmark or a guarantee of 60 fps on other workloads or hardware.

The machine ran Arch Linux with Mesa/vulkan-intel 26.2.2. Nested runs used an
existing Plasma Wayland session. Processes ran sequentially, with no other GPU
test processes active during these samples. Concurrent CPU activity was not controlled.

| Three-second workload | Before: frames | Before: mean / max submit/wait | After: frames | After: mean / max submit/wait |
| --- | ---: | ---: | ---: | ---: |
| Headless AXIAL, 240 fps cap | 142 | 20.66 / 55.87 ms | 301 | 9.74 / 30.62 ms |
| Nested AXIAL + native terminal + foot, 60 fps cap | 100 | 29.14 / 106.78 ms | 177 | 11.18 / 49.79 ms |

The logged timings are **CPU wall-clock intervals around `RenderFrame`**. They
include renderer uploads, driver submission and waits, and nested swapchain
acquisition/presentation calls. They are not GPU timestamp measurements,
complete frame times, or input-to-photon latency. Input handling, scene
construction and frame pacing occur outside this interval. The final snapshot
export is also excluded. The maxima still show occasional long submissions;
these samples do not establish sustained responsiveness.

## Commands used

These historical runs predate the general workspace default. Commands below
include `--experience=axial` to reproduce that same workload with the current CLI.

Build command, using the machine's unpacked Vulkan headers:

```sh
make build VULKAN_HEADERS=/tmp/Vulkan-Headers
```

The header override can be omitted when system Vulkan development headers are
installed. The same headless command was used before and after the change:

```sh
./bin/worldr-shell --experience=axial --backend=headless --width=2880 --height=1800 --fps=240 --duration=3s
```

Nested commands, before and after respectively:

```sh
./bin/worldr-shell --experience=axial --backend=nested --terminal --app=foot --duration=3s --snapshot=dist/worldr-workspace-current.png -- --config=/dev/null
./bin/worldr-shell --experience=axial --backend=nested --terminal --app=foot --duration=3s --snapshot=dist/worldr-workspace-gradient.png -- --config=/dev/null
```

There were no `--demo`, `--state`, or environment wrappers. Nested CLI dimensions
initially defaulted to 1440×900; the host configured the actual target to
2880×1800, confirmed by both exported image dimensions. The final run summary
now reports the actual render extent alongside sample count and timings. Match
that extent when comparing future runs; a differently configured host window
is a different workload.

## Renderer changes

`Canvas.RadialGradient` emits one bounded triangle fan, interpolating color and
alpha from center to perimeter using the existing coverage atlas. AXIAL's halo
previously shaded and blended ten overlapping filled discs. The replacement
reduces that alpha overdraw while retaining 4× MSAA and per-sample overlay
coverage. Tests check interpolation, clipping, single-layer coverage and
identical output across repeated frames.

World-space translucent guides also use explicit `DepthReadOnly` mesh draws.
The scene submits them after opaque meshes and application surfaces; they test
depth without writing it. This prevents faint guide lines from cutting holes
through content when viewed from below. Callers still own the mutual blend
order of overlapping translucent meshes; this is not general intersecting
transparency.

Ordinary nested presentation still uses the Vulkan swapchain without CPU image
readback. `--snapshot` performs a separate final offscreen export. See
[architecture](../ARCHITECTURE.md) for resource ownership and renderer limits,
and [run instructions](RUN-ABOX.md) for the available backends.

## Selective background glow

A separate three-second comparison on the same machine and date measured a
**paused, exploded, focused** AXIAL scene at 2880×1800, with 4× MSAA and a 240 fps
cap. No terminals or other GPU test processes were active. CPU activity remained
uncontrolled. This is a different workload from the moving scene above.

| Renderer | Frames | Mean / max CPU submit/wait |
| --- | ---: | ---: |
| Before authored glow and housing light bands | 416 | 6.81 / 22.15 ms |
| With authored glow and housing light bands | 283 | 10.47 / 32.17 ms |

The new effect has a measurable cost; these samples do not establish sustained
60 fps or input latency. The final implementation renders only the effect at
half resolution while retaining the main scene's full resolution and sample
count. It reuses the existing multisample color/depth attachments for the seed
pass, then applies two bounded blur passes and a background composite. Frames
with zero authored emission skip those passes. This is explicit background glow,
not an HDR bloom or color-management pipeline.

The later final-frame highlight bloom uses a fixed 13-tap presentation shader
and no additional images. The table above predates that shader and the moving
fill lights, so it must not be used as their performance result. A sustained
input-to-photon capture on representative hardware remains required.

The later hologram material also postdates this table. It reuses the bounded
depth-peeling targets and adds fixed per-fragment scan/contact math only for
explicit holographic meshes; zero hologram strength takes the established mesh
path. No standalone hologram performance result is claimed yet.

The comparison used identical saved scene settings. To reproduce the final
workload, create this document and run the command below:

```sh
mkdir -p dist
cat > dist/glow-performance.json <<'JSON'
{
  "version": 1,
  "experience_id": "worldr.axial",
  "state": {
    "version": 1,
    "selection": "rotor",
    "timeline": {"seconds": 6, "playing": false},
    "view": {
      "exploded": true,
      "focused": true,
      "camera": {"yaw": 0.93, "pitch": 0.32, "zoom": 0.07},
      "presentation": "cinematic",
      "panel_behind": true
    }
  }
}
JSON
./bin/worldr-shell --experience=axial --backend=headless --width=2880 --height=1800 --fps=240 --duration=3s --state=dist/glow-performance.json --snapshot=dist/worldr-glow-cinematic.png
```

The original comparison used `dist/axial-cinematic-preview.json`, with the same
settings and empty application placements. The baseline executable was retained
locally before this change; it is not a separately distributed build.
