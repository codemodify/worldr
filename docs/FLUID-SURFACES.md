# Native fluid surfaces

The Plasma playground reproduces the joining and separation behavior in the
user's eight-second Plasma UI reference video. Five independently draggable
panels share a continuous glass outline. Bringing panels close joins their
decoration; pulling one away separates it. This does not create a movement
group or merge their content.

```sh
./scripts/run-plasma-playground.sh
```

The launcher builds the native executable and keeps a separate layout in
`dist/plasma/playground.json`. It runs directly in Worldr's Vulkan renderer.
No browser, embedded webview, network service or image-generated UI is used.

Drag the panel contents to move them. **Fusion** enables joined outlines;
**Snap** controls magnetic edge/grid placement on release; **Motion** controls
spring settling and backdrop animation. The **Blend** slider changes joining
distance and **Glass** changes refraction. **Lumen**, **Studio**, and **Slate**
choose material/backdrop treatments. Reset restores the initial arrangement
and movement settings. Inbox, Tasks, and Player contain separate buttons with
local demonstration state; they do not start a window drag or perform external
mail/task/audio operations.

Tab selects a panel and the arrow keys move it. Enter or Space activates its
button, if present. Escape cancels a drag. F, S,
and M toggle fusion, snapping, and motion; 1–3 select appearance; R resets.
Ctrl+S saves, and Ctrl+Q exits. Positions, stacking, settings, appearance and
local button state restore on the next launch. An unfinished drag or slider
capture is excluded from a background checkpoint.

## Shared implementation

[`sdk/fluid/v1`](../sdk/fluid/v1/README.md) supplies bounded, renderer-independent
surface descriptions, rounded-box distance geometry, smooth union, proximity
queries, spring motion and snapping. A field contains at most 16 surfaces.
Visual fusion is independent of the existing workspace's explicit movement
groups. A surface can also opt out of fusion.

`render.FluidCommand` and `scene.Canvas.Fluid` record an ordered GPU field.
The Vulkan implementation uploads a small parameter block and draws the
procedural backdrop and shared glass material in one pass. Content images and
controls follow it in command order. Dragging changes parameters and image
placement; it does not rasterize the whole interface or rebuild/upload meshes.
The field samples its own procedural backdrop for refraction. It does not yet
sample arbitrary preceding scene content.

The playground retains shaped text images across movement and retires them
between frames when the output scale or bounded cache changes. Native resize,
device recovery, ordered overlays and multiple physical outputs use the same
rendering contract as the other experiences.

This release supplies the native playground and reusable rendering primitive.
Ordinary workspace windows keep their current frame behavior; connecting their
placement and depth rules to fluid fields is a separate integration. The
existing `plasma` skin remains compatible. `--skin=ID_OR_JSON` can map a skin's
background/accent colors onto the playground without resetting panel state.

The visual reference is [Plasma UI](https://github.com/CruxGarden/plasma-ui).
Worldr's geometry, shader and interaction implementation were written locally;
the React/WebGL library is not bundled.

## Captures and verification

```sh
./scripts/run-plasma-playground.sh --backend=headless --duration=3s \
  --snapshot=dist/plasma/preview.png --metrics=dist/plasma/metrics.json
./bin/worldr-shell --experience=plasma --backend=headless --demo \
  --width=884 --height=720 --frames=290 --snapshot=dist/plasma/demo.png
go test ./sdk/fluid/v1 ./internal/plasma ./internal/render ./internal/scene
WORLDR_TEST_GPU=1 go test ./internal/platform/linux/native -run Fluid
```

The deterministic demonstration uses the same panel movement model. Native
tests compare fusion and disabled-fusion pixels, content ordering, both color
modes, clipped fields, resize/recovery and retained resource counts. Interaction
tests cover capture/cancel, pulling one member away, snapping, keyboard movement
and transactional saved-state validation. Use the usual Vulkan header override
(`CGO_CFLAGS=-I/path/to/Vulkan-Headers/include`) when system headers are absent.

On the development machine, the fluid-only benchmark at 884×720 measured
median completed `RenderFrame` times of 1.045 ms for five surfaces and 1.503 ms
for sixteen (five runs of 100 draws, linear color, 4× MSAA, animated field,
readback disabled). These include submission/wait overhead but exclude the
playground's text and controls. Parameter packing allocated no memory; complete
render calls retained the renderer's existing three allocations / 560 bytes.
Raw results are in `dist/plasma/fluid-bench.txt`; these are local measurements,
not a frame-rate guarantee for other GPUs or resolutions.
