# Future Panels

Future Panels is a spatial `worldr-desktop` workspace with five real native
applications arranged at different depths: Files, Terminal, Research, Model
Inspector, and AXIAL / 07. Each window remains interactive and can be moved,
resized, thrown, minimized, or opened in Read view.

Run from the checkout:

```sh
./scripts/run-future-panels.sh
```

It also appears in **Experiments → Future Panels**, immediately before Workspace
Navigator. The standalone WorldR Terminal remains a separate application.

The visual direction comes from the [Future Panels reference clip](https://media.gettyimages.com/id/472500689/video/future-panels.mp4):
overlapping slate/indigo sheets, muted green text, thin edges, a distant light
source, and depth revealed by camera travel. The scene uses authored geometry
and textures; the video is not required at runtime.

Twenty background sheets share eight immutable textures and the same camera as
the applications. They are decorative spatial landmarks and never capture
input. Working windows retain their own alpha, opaque text and real content.
The field uses four transparency layers and performs no per-frame texture
painting or geometry creation. Its source textures rebuild only when the field
palette changes. Camera movement is manual, so typing never starts a camera
animation. Read view brings a window forward for comfortable reading.

The known thin window recipes also use a conservative screen coverage estimate
for transparency: a five-window fixture using the supplied arrangement needs
17 layers instead of the previous 32. Overlap and app-owned translucent 3D
objects increase that budget automatically; custom geometry keeps the general
renderer allowance. GPU comparisons against
32 layers are pixel-identical across overlap, view movement, Read and resize.

A 120-frame headless run with all five real apps at 1440×900 and 4× sampling on
Intel Graphics (ARL) measured 15.25 ms mean render submit/wait, 16.0 ms p95,
and about 177 MiB of owned Vulkan allocations. These are CPU wall-clock timings
on this machine, not input-to-display latency or a frame-rate guarantee. The
report is written with `--metrics`; the first shader/resource setup frame is
included. The synthetic full-depth comparison includes CPU readback and is a
different workload.

The appearance is also available under **Settings → Skins → Future Panels**,
including its portable window/control recipes and palette. Switching skins
keeps placements and environment preferences intact.

## Mouse and keyboard

- Drag a window's top grip to move it; a quick release gives it momentum. Grab
  the moving window again to stop it.
- Hold Super and drag inside a window to move it. Scroll while holding it to
  move through depth and release to throw it into the workspace.
- Super+wheel changes the hovered window's depth without taking its focus.
- Drag the lower-right resize grip to resize the application.
- Use the square window control or Super+double-click to enter or leave Read
  view for focused work. Normal dragging inside an application still belongs
  to that application.
- Hold Super and drag empty space to walk through the workspace. The scene
  controller in the lower-right corner changes where you look; its X resets the
  camera. An unmodified wheel over empty space moves the camera forward/back.
- Ctrl+Alt+O opens the window overview. Use arrows to select and Enter to return
  to that window's workspace. Ctrl+Alt+Enter opens another terminal.
- Ctrl+Z undoes a placement gesture, including its completed throw. F1 opens the
  full workspace controls.

## Saved workspace

The layout and native application session live in
`dist/future-panels/workspace.json`, independently of the earlier experiments.
Starter applications open only on the first launch, an untouched failed-launch
retry, or explicit `--fresh`. Existing recovery checkpoints and intentionally
empty sessions are respected.

```sh
# Keep the saved arrangement but reopen the five starter applications.
./scripts/run-future-panels.sh --fresh

# Start an independent workspace with the supplied layered arrangement.
./scripts/run-future-panels.sh --state=dist/future-panels/another-session.json
```

The launcher accepts normal `worldr-desktop` options after its defaults. The
starter layout is authored in `examples/future-panels/workspace.json`; changing
that file affects new sessions, not existing saved placements.
