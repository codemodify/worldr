# Fluid panels v1

`github.com/codemodify/worldr/sdk/fluid/v1` is a public, standard-library-only package for rounded panel geometry, visual joining, and independent motion. It imports no renderer, application, or internal package. Native renderers consume the same `Field` that CPU hit tests sample.

```go
field := fluid.Field{
    Bounds: fluid.Rect{X: 0, Y: 0, Width: 884, Height: 720},
    Style: fluid.DefaultStyle(),
    Surfaces: []fluid.Surface{
        {Bounds: fluid.Rect{X: 40, Y: 140, Width: 240, Height: 180},
            Radius: 28, Tint: [4]float32{0.35, 0.7, 0.9, 0.6}, Fuse: true},
        {Bounds: fluid.Rect{X: 292, Y: 160, Width: 240, Height: 180},
            Radius: 28, Tint: [4]float32{0.7, 0.45, 0.9, 0.6}, Fuse: true},
    },
}
if err := field.Validate(); err != nil {
    return err
}
inside := field.Contains(fluid.Point{X: 286, Y: 230})
```

All positions, rectangle extents, corner radii, blend distances, and the pointer use one framebuffer coordinate space. Scale them together for DPI changes. `Field.Bounds` is the viewport and clip; panels can extend outside it while being dragged. `Field.Distance` samples the shape without clipping, while `Field.Contains` also checks the half-open viewport rectangle. An empty field has an infinite shape distance and can still render its background.

`Surface.Tint` and `Style.Background` contain straight RGBA channels in `[0,1]`. `Blend` is in framebuffer units; `Rim`, `Refraction`, `Frost`, `Glow`, and `Opacity` are normalized material intent for a renderer. `Opacity=0` makes the glass invisible. All-zero style values remain meaningful. Defaults are explicit: blend 28, rim .55, refraction .35, frost .25, glow .15, opacity .68, with a blue/purple three-color background.

Call `Validate` once when accepting or changing a field. It rejects more than 16 surfaces, nonfinite numbers, coordinates/extents outside ±1,048,576, nonpositive dimensions, dimensions above 32,768, radii larger than half the short side, blend above 1,024, invalid normalized channels, and times outside 0–1,000,000 seconds. `Point`, `Rect`, `Surface`, and `Style` also expose validation. The surface slice is borrowed; `Field.Clone()` creates owned storage for retained or asynchronous use.

The rounded-box signed distance is negative inside and zero at the contour:

```text
q = abs(point - center) - (halfSize - radius)
d = length(max(q, 0)) + min(max(q.x, q.y), 0) - radius
```

Only `Fuse=true` panels participate in smooth union. Fold them in slice order, using:

```text
h = max(blend - abs(a - b), 0) / blend
union = min(a, b) - blend * h * h * .25
```

For zero blend, use a hard minimum. Combine all non-fusing panels by hard minimum after the smooth fold. This prevents a non-fusing panel from creating a bridge. CPU and GPU implementations should retain the same expression and ordering: repeated polynomial smooth union is not associative.

`Separation(a,b)` returns the signed gap between two rounded boxes, accounting for their corners. `Joined(a,b,blend)` reports pairwise visual adjacency when both panels allow fusion and that gap is at most `blend/2`. A chain may therefore join through neighboring pairs. These functions never form a movement group or move another panel. The pairwise query is distinct from evaluating the complete, ordered multi-panel field.

`Spring{Position,Velocity}` stores presentation motion separately from logical layout. `Step(target, dt, frequency, damping)` uses an analytic damped-spring solution: seconds, Hz, and damping ratio. Damping 1 is critical; lower values may overshoot. It supports frequency `(0,120]`, damping `[0,4]`, and steps `(0,10]` seconds; invalid/nonfinite inputs and a zero step are no-ops. `Reset(point)` clears momentum, suitable for immediate placement or reduced motion.

`SnapGrid(point, spacing)` rounds to the nearest grid multiple, with half values away from zero. `SnapMagnetic(bounds, peers, distance)` selects the nearest edge, center, or adjacent edge on each axis when the peer is also near on the perpendicular axis. It preserves size, considers up to 16 peers, uses stable caller-order tie breaking, and reports whether a target matched. `ClampRect(bounds, container)` then fits a panel inside its area, shrinking oversized dimensions only when necessary. These helpers do not retain state or mutate peer rectangles.

```sh
go test ./sdk/fluid/v1
```

Tests cover rounded corners and negative coordinates, clip semantics, pairwise adjacency and three-panel chains, disabled fusion, live geometry changes, finite limits and ownership, frame-rate-independent springs, damping/reset behavior, snapping ties, and bounding rectangles.
