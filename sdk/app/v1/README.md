# Standalone GPU applications

Import `github.com/codemodify/worldr/sdk/app/v1` as `app` to run a native
Wayland/Vulkan application independently of worldr-desktop. The initial runtime
targets Linux with CGO, Vulkan, Wayland, and XKB development dependencies.

Implement `Application` with `Atlas`, `Draw`, `Update`, `Handle`, and `Close`, then
call `app.Run(ctx, app.Options{Title: "My app", Width: 1100, Height: 720}, instance)`.
`NewCanvas` provides GPU vector primitives and an embedded font coverage atlas;
`NewCanvasWithFont` uses your TTF/OTF font bytes. `Texture` and `Geometry` retain
uploaded images and meshes. Frames go directly to the GPU swapchain.

All application methods execute on the runtime's goroutine. `Draw` receives
framebuffer dimensions; native pointer coordinates use the same pixels. Optional
`SetScale(float32)` receives the logical-to-framebuffer scale before drawing, so
controls and terminal cells remain physically legible on high DPI displays.

Optional `SetHost(*app.Host)` receives move, resize, minimize, maximize, close,
title, and clipboard operations. Move and resize must start from a pointer press.
Clipboard reads are asynchronous and bounded to 4 MiB; completion runs on the
application goroutine. Key events preserve raw Linux keycodes and XKB state,
allowing terminal applications to implement their own key translation and repeat.

`Transparent: true` and `ClientDecorated: true` allow your app to render its own
frame over a genuinely alpha-composited window. Use `Canvas.SetLinearColor(true)`
for transparent content. Desktop wallpaper comes from the compositor; blur behind
the window depends on the compositor and is not synthesized by this runtime.

`NeedsFrame() bool` can skip GPU submissions while idle. `Update` and input still
run at the configured `FPS` (default 60); worker goroutines can call `Host.Wake()`
to request a frame. Draw buffers are borrowed until synchronous submission
completes. GPU textures and geometry absent from a newly submitted frame retire
only after their in-flight uses have completed. Run closes the application on
every exit, including errors during startup.

Applications caching textures across views may implement
`RetiredTextures() []uint64`. This opts into explicit texture lifetimes: previously
submitted textures remain resident until that method returns their IDs after a
frame no longer references them, or the GPU session closes. The runtime consumes
retirement IDs after each successful submission. Geometry still retires when
absent from the current frame.

`Headless`, `Frames`, `Duration`, and `Snapshot` support rendering checks. An
explicit frame count requests that many submissions even for an idle application.
Headless mode defaults to one frame when neither limit is given. Snapshot PNGs
preserve alpha; normal interactive rendering performs no pixel readback.
