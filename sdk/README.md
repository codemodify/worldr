# worldr-kit

WorldR's toolkit for building GPU-rendered applications. This directory contains
the current public SDK building blocks; see the [product boundary and first
usable milestone](../docs/PRODUCTS.md) for the extraction plan.

| Package | Available today |
| --- | --- |
| [`sdk/app/v1`](app/v1/README.md) | Standalone Wayland/Vulkan application runtime, GPU canvas, alpha windows, input, clipboard and headless snapshots |
| `sdk/nativeapp/v1` | Versioned external application contract, retained resource updates, input, lifecycle and semantics |
| [`sdk/nativeui/v1`](nativeui/v1/README.md) | Portable controls, interaction and CPU RGBA painting |
| [`sdk/skin/v1`](skin/v1/README.md) | Shared skin descriptions, metrics, colors and appearance recipes |
| [`sdk/fluid/v1`](fluid/v1/README.md) | Renderer-independent fluid surface geometry and interaction |

These packages retain their existing imports under
`github.com/codemodify/worldr/sdk/...`. The standalone runtime exposes the shared
canvas and retained render types through a public facade. Their Vulkan backend
and implementation remain in internal packages; applications using this runtime
do not import the desktop or workspace packages.

The toolkit's intended hosts are ordinary native application windows and
worldr-desktop. Applications should not need to import desktop navigation,
workspace placement, or experimental presentation code.
