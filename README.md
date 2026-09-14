# worldr

Linux-first cinematic desktop: owned Vulkan engine + Wayland compositor.
The UI toolkit is deferred. Foreign Wayland and X11 clients are the first apps.

**Status:** Phase 0 — license, locked decisions, Go module layout. No compositor or Vulkan implementation yet.

## Quick links

- [ARCHITECTURE.md](ARCHITECTURE.md) — locked product decisions, layers, non-goals
- [LICENSE](LICENSE) — The Free License (TFL)
- [cmd/worldr-shell](cmd/worldr-shell) — future compositor binary
- [cmd/worldr-session](cmd/worldr-session) — future session manager

## Build order

0. This scaffold
1. DRM/KMS + Vulkan clear-to-screen in Go
2. Minimal Wayland server (surface → textured window actor)
3. SSD borders + input/focus
4. XWayland
5. Compiz-style effect graph
6. Revisit UI toolkit

## Build

Requires Go 1.22 or later. Placeholder binaries print a version and exit.

```sh
make build   # bin/worldr-shell, bin/worldr-session
make test
make fmt
```

Or: `go build ./cmd/...`

## License

[The Free License](https://licenseplanet.net/the-free-license). See [LICENSE](LICENSE).
