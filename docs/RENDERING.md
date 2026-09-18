# Native rendering contract

Worldr records retained mesh and texture instances through `scene.Scene` and
`scene.Canvas`; `render.Frame` preserves camera, overlay and image order.
The Vulkan backend owns GPU allocations. Moving an instance changes constants,
not its geometry. A changed texture uploads its damaged rectangle. Providers
retire resources with `ReleaseGeometry` / `ReleaseTexture` after use.

## Linear color

```go
canvas.SetLinearColor(true) // retained across Canvas.Reset
```

The equivalent low-level flag is `render.Frame.LinearColor`. The zero value
preserves the original renderer and existing workspace appearance. Linear mode
applies to the entire ordered frame, including overlays and app surfaces.

Author RGB colors, texture bytes and clear RGB in sRGB. Mesh and tint colors use
straight alpha. `ImageCommand` and translucent texture surfaces require
premultiplied sRGB RGBA8; ordinary opaque surfaces ignore texture/tint alpha.
Alpha represents linear coverage. The renderer decodes before interpolation,
lighting and compositing, including unassociation/reassociation for premultiplied
images. Hierarchical scene tints multiply in linear light when the canvas opts in.

Linear frames use RGBA16F targets so blending and multisample resolves happen in
linear light. A final output shader encodes SDR sRGB once; readback returns
premultiplied sRGB BGRA8, including transparent captures. Headless rendering runs
the same output shader. Display selection requires an SDR sRGB color-space pair.
With the zero output transform, opaque photo/video pixels round-trip; lighting
and partial-coverage edges will look different from encoded-RGB blending.
Physical output remains SDR sRGB;
there is no HDR signaling, ICC profile conversion, display calibration, or
wide-gamut pipeline.

## SDR output finish and highlight bloom

`render.Frame.Output` (or `Canvas.SetOutputTransform`) applies a bounded,
display-referred finish after the complete ordered frame has been composed:

```go
canvas.SetOutputTransform(render.OutputTransform{
    Exposure: .05, Saturation: .08, Contrast: .06,
    ToneMap: render.ToneMapFilmic,
    BloomStrength: .32, BloomThreshold: .72, BloomRadius: 5,
})
```

The zero value follows the original output shader exactly. Exposure is `[-4,4]`
stops; saturation and contrast are signed `[-1,1]` adjustments. The optional
filmic curve is a compact SDR look transform, not an ACES implementation.
Bloom thresholds the final linear scene, then samples a fixed 13-tap disc;
strength is `[0,2]`, threshold `[0,8]`, and radius `[0,32]` output pixels (zero
selects four while bloom is active). Its shader cost is constant and it allocates
no additional images. Output controls reject encoded-RGB frames because exposure,
thresholding, and grading there would have undefined color meaning.

This highlight bloom is separate from per-mesh `Draw.Glow`. Glow is an authored,
depth-aware halo behind one camera pass. Bloom sees the final composed frame and
can respond to bright mesh, app, glow, and overlay pixels. On a multi-device
desktop each output finishes its own cropped image, so the bloom kernel stops at
the physical output boundary. Because it intentionally changes those pixels and
can spill beyond a bright overlay, leave it disabled on compositor frames that
promise exact client/UI color or append a host cursor after frame construction.

## Fill lights, thin glass and holographic projections

`Scene.PointLights` copies up to four `render.PointLight` values into each camera
command. Position and radius are world-space; color is authored sRGB; intensity
is bounded to `[0,8]`. The mesh shader applies quadratic radius falloff to diffuse
and material highlights. Fill lights do not cast shadows and do not affect unlit
meshes, content textures, geometry residency, depth, or picking. Moving a light
only updates instance constants.

`Material.Transmission` removes up to all but ten percent of a mesh's diffuse
body response while keeping its direct specular and rim accents. It is accepted
only on a `Translucent` mesh, so it composes through the same bounded depth-peeling
path as other transparent geometry. A practical thin-glass material combines a
low draw alpha with high transmission/specular, a nonzero roughness, and a
restrained colored rim.

`Material.Refraction` in `[0,1]` opts that glass into sampling the opaque scene
behind it. It requires a lit, translucent geometry draw with nonzero
`Transmission`; at one it applies at most an 18-output-pixel, smooth-normal
directed bend. `Material.RefractionBlur` in `[0,1]` requires nonzero
`Refraction` and adds a fixed five-tap frosted footprint of at most six output
pixels. These are bounded material controls, not a general post-process.

The renderer captures one opaque-scene color prepass and samples it only under
explicitly refractive native glass. Other transparent layers, screen-space
overlays and a host cursor appended after frame construction are not part of
that backdrop. Client and legacy texture uploads are never rewritten; outside
the glass coverage their pixels remain exact. Refraction can visibly displace
an opaque application surface where authored native glass covers it. With
`Refraction` and `RefractionBlur` both zero, no backdrop sample is mixed, which
preserves the previous transmission-only result and exact client/legacy texture
pixels.

`Material.Hologram` in `[0,1]` opts a `Translucent` native mesh into a bounded
projection treatment. It combines a view-angle silhouette, two world-space scan
frequencies and a narrow highlight where the fragment approaches the opaque
scene depth. `RimColor` supplies the projection tint; an all-zero rim uses the
mesh color. The mesh still goes through the existing depth-peeling budget, so
opaque geometry occludes it and its front/back surfaces compose at their actual
depths. This is a surface treatment with an opaque-depth contact cue, not
participating-media simulation, ray marching, volumetric scattering or
physically based emission.

`Scene.EffectPhase` (the low-level `View.EffectPhase`) is a normalized `[0,1]`
scan phase. Changing it updates constants only. The workspace advances the phase
on its transient presentation clock and freezes the current value under Reduced
Motion; model, data and media playback continue. Holograms allocate no targets
beyond those already required by translucent geometry. They cannot be attached
to an application texture or screen overlay, and the host cursor uses a separate
path. With `Hologram == 0`, neither coverage nor color changes and the established
mesh/client/UI result remains exact.

## Directional shadows

```go
light := scene.Vec3{X: -.35, Y: .75, Z: .8}
shadow, err := scene.DirectionalShadow(center, light, 12, 18)
if err != nil { return err }
world.Light = light
world.Shadow = shadow
world.Node(model).CastShadow = true
world.Node(floor).ReceiveShadow = true
```

`DirectionalShadow` produces an orthographic light projection; `span` is its
width/height, `depth` its extent along the light direction. Fit it around the
objects that need shadows. Low-level camera passes use `render.View.Shadow`:
`Projection` maps world points to Vulkan clip coordinates; `Strength` is `[0,1]`
(zero disables); receiver `Bias` is `[0,.05]` in normalized light depth.
Defaults from the helper are strength `.8` and bias `.0015`.

Each shadowed camera has a retained 1024×1024 depth map with a bounded nine-tap
receiver filter. At most four shadowed camera commands are accepted per frame.
Outside the light volume receivers stay lit. Casting/receiving flags belong to
each node and are not inherited. Opaque content can cast its rectangular
silhouette; app pixels receive no shadow unless explicitly opted in. Unlit
meshes ignore received shadows. Translucent casters are rejected. This provides
direct-light shadows, not ambient occlusion, global illumination, area lights,
or transparent colored shadows.

## Spatial transparency

Set `scene.Node.Translucent` (or `render.Draw.Translucent`) explicitly. The default
opaque app contract stays unchanged. Transparent draws run after opaque content
and existing `DepthReadOnly` guides; they test opaque depth without modifying the
main depth buffer. Mesh colors use straight RGBA. Texture colors are premultiplied
RGBA, modulated by straight tint RGB and tint alpha exactly once.

The backend uses front-to-back depth peeling rather than one object sort order.
Crossing panels and self-overlapping meshes therefore compose per pixel.
`Scene.TransparencyLayers` / `render.View.TransparencyLayers` selects `[1,32]`
layers; zero means eight. Additional layers are omitted behind the nearest
retained layers. Exactly coincident equal-depth fragments resolve as one surface;
avoid placing overlapping translucent layers at identical depth. Transparent
coverage is currently single-sampled; ordinary meshes and overlays retain their
existing multisampling. Picking still uses the full logical content plane,
including transparent texels.

A frame accepts at most four transparent camera commands. Three depth scratch
images, one current-layer image and one opaque-backdrop color target are reused
across views; each view retains one accumulated color target. The shared backdrop
is included in the transparency memory accounting, and total pixel storage is
capped at 256 MiB. Invalid settings and unsupported allocation sizes return
visible errors. `Translucent` cannot be combined with `DepthReadOnly` or
`CastShadow`.

## GPU application buffers

`VK.DMABufFormats()` reports the supported client import pairs for that Vulkan
device. The current subset is one memory plane, an explicit DRM modifier, and
the ARGB/XRGB/ABGR/XBGR families in 8888 and 2101010 layouts, up to 4096×4096.
This includes `DRM_FORMAT_MOD_LINEAR` and device-specific non-LINEAR modifiers
when Vulkan reports one modifier plane, transfer-source use and DMA-BUF import
for the exact pair. A pair is advertised only when the same device can also
copy it into an importable/exportable LINEAR image and sample the result.
Advertisement is bounded to 256 pairs. Unsupported devices advertise no
GPU-sharing pairs and application hosting keeps its SHM path. Multi-plane YUV,
modifiers with multiple memory planes, implicit/invalid modifiers, explicit
Wayland syncobj synchronization, direct scanout, and arbitrary cross-GPU
combinations are outside this subset.

When the importer supplies its validated DRM render node, the application
server advertises linux-dmabuf version 4. Its sealed memfd format table contains
only those exact format/modifier pairs, and both main-device and tranche-device
feedback identify that renderer node. The compatibility importer without a
device identity advertises version 3. Xwayland requests glamor only when its
binary supports it, the render node is accessible and XRGB8888 and ARGB8888
share one advertised explicit modifier; startup failure retries once with SHM.

`VK.ImportDMABuf(dmabuf.Descriptor)` borrows the client's FD synchronously. It
validates layout/size and Vulkan FD compatibility, waits at most two seconds for
the implicit producer fence, imports dedicated memory, acquires foreign queue
ownership, and copies the pixels on the GPU. It then releases queue ownership
and waits for completion before returning a new immutable `render.Texture`.
Only successful return permits the application server to release `wl_buffer`.
No CPU mapping or pixel readback is used in this path.

The texture owns an independent exported LINEAR GPU snapshot, so client buffer reuse
cannot mutate an earlier frame. Every rendering device imports and GPU-copies
this snapshot into its own sampled image; snapshots and device recovery therefore
work without keeping a client's buffer busy. Close the texture once no future
frame refers to it, and retire its ID through each device's `ReleaseTexture`.
Closing a Vulkan device releases that device's copies without closing shared
textures. Ordinary CPU textures retain their existing behavior.

Imported/transient GPU allocations count against the per-device budget. Exported
snapshot FDs retain memory independently of a device; they share an additional
256 MiB cap reported by `dmabuf.RetainedBytes()`. Explicit `Texture.Close()` releases
that accounting and FD; a finalizer is only a fallback. ARGB and ABGR retain
premultiplied coverage; XRGB and XBGR force alpha one. `scene.Node.SurfaceUV` / `render.Draw.UV`
optionally crop a surface with normalized `{x,y,width,height}`; zero means the
whole image. Cropping changes sampling, not geometry or logical picking.

The import/export path follows the [Vulkan DRM-modifier contract](https://docs.vulkan.org/refpages/latest/refpages/source/VK_EXT_image_drm_format_modifier.html),
[FD ownership rules](https://docs.vulkan.org/refpages/latest/refpages/source/VkImportMemoryFdInfoKHR.html),
and [Linux implicit-fence polling semantics](https://docs.kernel.org/driver-api/dma-buf.html#implicit-fence-poll-support).

The real modifier gate compiles an independent GBM producer, allocates an
advertised non-LINEAR pair when the device exposes one, sends the exact modifier
through the Wayland protocol, verifies the rendered pixels and checks that the
owned snapshot returned to the renderer is LINEAR. `WORLDR_TEST_DMABUF=1` runs
the workflow when supported. Add `WORLDR_TEST_DMABUF_MODIFIERS=1` to require a
non-LINEAR pair instead of accepting a hardware-dependent skip.

## Allocation budgets and recovery

`VK.SetMemoryBudget(bytes)` limits renderer-owned Vulkan device-memory
allocations; zero restores the 1 GiB default. `VK.MemoryStats()` reports actual
allocation requirements, peak usage, budget, and live image/buffer counts.
Accounting includes retained geometry/textures, staging, uniforms, atlas, render
targets, and effects. Driver/swapchain allocations and Go CPU copies are outside
this budget. Lowering the budget below current use fails without changing it.

Allocation failures preserve the current atlas. Failed headless resize and color
mode changes attempt to restore the previous usable targets. Supported extents
are additionally bounded by device limits and 7680×4320; a zero resize returns
`ErrNotReady` without destroying the live extent. A failed display replacement
marks the renderer as requiring recovery rather than submitting an incomplete
target.

Errors support `errors.Is` with `ErrDeviceLost`, `ErrOutOfMemory`,
`ErrSurfaceLost`, `ErrGPUTimeout`, `ErrNeedsRecovery`, `ErrOutOfDate`, and
`ErrNotReady`. `VulkanError` retains the numeric Vulkan result and operation.
`VK.Recover()` recreates the logical device and swapchain on the same physical
device/surface, resets residency caches, and restores an owned copy of the last
successful atlas. The next frame reuploads retained CPU geometry and textures;
the configured budget and session peak remain intact. Call these methods only
on the render goroutine. The host must bound retry attempts and continue saving
workspace state independently of graphics recovery.

On a multi-output direct session, each render failure carries its connector ID.
The host recreates only that output's logical device and retained residency;
healthy outputs keep their devices and resources. A failure without connector
context still triggers a conservative all-output rebuild. Unit coverage injects
a device loss into the second output, verifies that the first output's recovery
counter remains untouched, then presents successfully on both outputs again.

Recovery does not reconnect removed monitors or recreate a lost host surface.
Vulkan device-idle/destruction calls have no portable timeout, so a driver blocked
inside one cannot be repaired through an in-process retry. Tests exercise actual
device recreation and deterministic allocation failures; physical GPU resets,
display hotplug, and suspend/resume remain hardware validation work.

The display-manager entry point runs the shell in a distinct process group. On
session termination it forwards the first signal, permits the shell's normal
application and graphics cleanup for `--shutdown-timeout` (eight seconds by
default), then kills that process group. A second signal bypasses the grace
period. The leader remains waitable but unreaped until group cleanup, which
pins the process-group number and prevents a fast PID reuse from redirecting
the final signal. Straggling helpers are killed even when the shell leader
exits first, and inherited output pipes have a separate finite wait bound. The
supervising process returns after a second bounded wait even if a driver call
never returns. This bounds the login session manager; it cannot make a wedged
kernel driver recover in process. If the supervisor itself exits unexpectedly,
Linux applies a parent-death kill to the shell so it cannot remain as an
orphaned display owner; the Go thread which created it remains pinned for the
shell lifetime because Linux ties that guarantee to the creating thread. The
ordinary signal path remains graceful and allows the workspace checkpoint to
complete.

## Verification

`internal/platform/linux/native` contains GPU pixel tests for opaque image/text
preservation, linear filtering and blending, premultiplied transparent readback,
SDR grading, highlight bloom, moving point lights, thin-glass transmission,
depth-composited holograms and their opaque contact/overlay isolation,
bounded backdrop refraction and frosted blur, shadow movement/independent cameras,
intersecting panels, self-overlapping meshes,
opaque occlusion, finite layer budgets, resize/resource retention, allocation
budget rollback, atlas ownership, and repeated device recreation. Existing
legacy rendering tests remain active. Shader sources live in `shaders/`; run
`make shaders` after editing them to regenerate checked-in SPIR-V.

DMA-BUF integration tests compile an independent GBM producer, pass its real
exported FD across a Unix socket, then verify BGR and RGB byte-order pixel colors,
client reuse, premultiplied coverage, XRGB alpha, UV crops, a second renderer,
device recovery, advertised 2101010 channel order and X alpha, and allocation/FD ownership failures.
`WORLDR_TEST_DMABUF=1` makes missing GBM
development files, render-node access, or required sharing support a test failure;
otherwise unavailable environments skip those integration cases.

The AXIAL hologram authoring was also reviewed in a 1440×900, 45-frame headless
capture on the reported Intel ARL Vulkan device:

```sh
./bin/worldr-shell --backend=headless --experience=axial \
  --frames=45 --fps=60 --width=1440 --height=900 \
  --snapshot=dist/worldr-hologram-reviewed.png
```

The reviewed PNG had SHA-256
`4c0dc509cb01f1fadec0a88b4a7d8979fea2170e7e89da03a8ce9a04cfb21acc`.
The enlarged housing shell showed cyan scan bands and contact rims while the
opaque rotor/shaft, instrument text and screen overlays remained crisp. This is
a visual regression observation, not a colorimetric or performance result.

The implementation follows the [Vulkan blending contract](https://docs.vulkan.org/spec/latest/chapters/framebuffer.html),
[SDR color-space semantics](https://docs.vulkan.org/refpages/latest/refpages/source/VkColorSpaceKHR.html),
and [Khronos depth-peeling approach](https://docs.vulkan.org/samples/latest/samples/api/oit_depth_peeling/README.html).
