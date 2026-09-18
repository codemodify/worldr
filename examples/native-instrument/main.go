// Command native-instrument is a minimal out-of-process Worldr-native app.
// It imports only the public v1 SDK and the Go standard/x/image libraries.
package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
)

const (
	textureID nativeapp.ResourceID = 1
	meshID    nativeapp.ResourceID = 2
	glassID   nativeapp.ResourceID = 3
	surfaceID nativeapp.SurfaceID  = 1
)

type instrument struct {
	host                nativeapp.Host
	image               *image.RGBA
	pending             []nativeapp.TextureUpdate
	revision            uint64
	elapsed, paintClock time.Duration
	running, focused    bool
	closed, retired     bool
}

func (a *instrument) Manifest() nativeapp.Manifest {
	return nativeapp.Manifest{
		ID: "dev.worldr.example.instrument", Name: "Orbital instrument",
		Description: "A bounded native-app SDK v1 telemetry example",
	}
}

func (a *instrument) Start(host nativeapp.Host) error {
	a.host, a.running, a.revision = host, true, 1
	a.image = image.NewRGBA(image.Rect(0, 0, min(760, host.MaxSurfaceWidth), min(460, host.MaxSurfaceHeight)))
	a.paint(true)
	return nil
}

func (a *instrument) Update(delta time.Duration) error {
	if a.closed || !a.running {
		return nil
	}
	a.elapsed += delta
	a.paintClock += delta
	if a.paintClock >= 80*time.Millisecond {
		a.paintClock %= 80 * time.Millisecond
		a.revision++
		a.paint(false)
	}
	return nil
}

func (a *instrument) Snapshot() nativeapp.Snapshot {
	updates := a.pending
	a.pending = nil
	if a.closed {
		result := nativeapp.Snapshot{Textures: updates}
		if !a.retired {
			result.RetireTextures = []nativeapp.ResourceID{textureID}
			result.RetireMeshes = []nativeapp.ResourceID{meshID, glassID}
			a.retired = true
		}
		return result
	}
	w, h := a.image.Bounds().Dx(), a.image.Bounds().Dy()
	objects := []nativeapp.SpatialObject{{
		ID: 1, Mesh: meshID, Transform: modelMatrix(float32(a.elapsed.Seconds()) * .7),
		Color:     nativeapp.Color{R: .08, G: .55, B: .75, A: 1},
		WireColor: nativeapp.Color{R: .2, G: .95, B: 1, A: 1}, WireWidth: 1.25,
		Material: nativeapp.Material{Specular: .8, Roughness: .2, Metallic: .75, RimStrength: .9, RimColor: [3]float32{.05, .8, 1}},
		Glow:     [3]float32{0, .35, .55}, CastShadow: true, ReceiveShadow: true,
	}, {
		ID: 2, Parent: 1, Mesh: glassID, Transform: scaleMatrix(1.22),
		Color:     nativeapp.Color{R: .16, G: .76, B: .92, A: .34},
		WireColor: nativeapp.Color{R: .25, G: .92, B: 1, A: .28}, WireWidth: .55,
		Material:    nativeapp.Material{Specular: .9, Roughness: .14, RimStrength: .65, RimColor: [3]float32{.12, .85, 1}, Transmission: .96, Refraction: .78, RefractionBlur: .14},
		Translucent: true, Unpickable: true,
	}}
	return nativeapp.Snapshot{
		Textures: updates,
		Meshes: func() []nativeapp.MeshResource {
			if a.revision != 1 || len(updates) == 0 {
				return nil
			}
			return []nativeapp.MeshResource{instrumentMesh(), instrumentGlassMesh()}
		}(),
		Surfaces: []nativeapp.Surface{{
			ID: surfaceID, Key: "orbital", Title: "ORBITAL / LIVE TELEMETRY", Texture: textureID,
			FrameStyle: nativeapp.FrameCinematic,
			Spatial:    &nativeapp.SpatialContent{Objects: objects, Labels: []nativeapp.SpatialLabel{{Text: "VECTOR CORE", Position: nativeapp.Vec3{X: .31, Y: .31, Z: .1}, Color: nativeapp.Color{R: .3, G: .95, B: 1, A: 1}}}},
			Semantics: nativeapp.SemanticTree{Nodes: []nativeapp.SemanticNode{
				{ID: "telemetry", Role: nativeapp.RoleImage, Label: "Orbital telemetry plot", Bounds: nativeapp.Rect{X: 28, Y: 82, Width: max(1, w-56), Height: max(1, h-154)}},
				{ID: "run", Role: nativeapp.RoleButton, Label: map[bool]string{true: "Pause telemetry", false: "Resume telemetry"}[a.running], Bounds: nativeapp.Rect{X: 28, Y: h - 54, Width: 146, Height: 30}},
			}},
		}},
	}
}

func (a *instrument) Handle(id nativeapp.SurfaceID, event nativeapp.Event) error {
	if id != surfaceID || a.closed {
		return nil
	}
	activate := event.Kind == nativeapp.PointerDown && event.Button == nativeapp.ButtonPrimary && event.X >= 28 && event.X < 174 && event.Y >= float32(a.image.Bounds().Dy()-54) && event.Y < float32(a.image.Bounds().Dy()-24)
	activate = activate || event.Kind == nativeapp.KeyInput && event.Pressed && !event.Repeat && event.Key == "Space"
	if activate {
		a.running = !a.running
		a.revision++
		a.paint(true)
	}
	return nil
}

func (a *instrument) Focus(id nativeapp.SurfaceID) error {
	focused := id == surfaceID
	if focused != a.focused && !a.closed {
		a.focused = focused
		a.revision++
		a.paint(true)
	}
	return nil
}

func (a *instrument) Resize(id nativeapp.SurfaceID, width, height int) error {
	if id != surfaceID || a.closed {
		return nil
	}
	width, height = min(a.host.MaxSurfaceWidth, max(480, width)), min(a.host.MaxSurfaceHeight, max(300, height))
	if width == a.image.Bounds().Dx() && height == a.image.Bounds().Dy() {
		return nil
	}
	a.image = image.NewRGBA(image.Rect(0, 0, width, height))
	a.revision++
	a.paint(true)
	return nil
}

func (a *instrument) CloseSurface(id nativeapp.SurfaceID) error {
	if id == surfaceID {
		a.closed = true
		a.pending = nil
	}
	return nil
}

func (a *instrument) Close() error { return nil }

func (a *instrument) paint(full bool) {
	bounds := a.image.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	plot := image.Rect(28, 82, w-28, h-72)
	damage := plot
	if full {
		damage = bounds
		draw.Draw(a.image, bounds, &image.Uniform{C: color.RGBA{3, 12, 22, 255}}, image.Point{}, draw.Src)
		for x := 0; x < w; x += 32 {
			line(a.image, x, 0, x, h-1, color.RGBA{8, 42, 58, 255})
		}
		for y := 0; y < h; y += 32 {
			line(a.image, 0, y, w-1, y, color.RGBA{8, 42, 58, 255})
		}
		strokeRect(a.image, image.Rect(10, 10, w-10, h-10), color.RGBA{25, 205, 235, 255}, 2)
		label(a.image, 28, 42, "ORBITAL RESEARCH ARRAY", color.RGBA{110, 235, 255, 255})
		label(a.image, 28, 62, "SDK V1  //  RETAINED TELEMETRY", color.RGBA{40, 140, 175, 255})
	}
	draw.Draw(a.image, plot, &image.Uniform{C: color.RGBA{2, 20, 31, 255}}, image.Point{}, draw.Src)
	for x := plot.Min.X; x < plot.Max.X; x += 40 {
		line(a.image, x, plot.Min.Y, x, plot.Max.Y-1, color.RGBA{8, 55, 70, 255})
	}
	for y := plot.Min.Y; y < plot.Max.Y; y += 36 {
		line(a.image, plot.Min.X, y, plot.Max.X-1, y, color.RGBA{8, 55, 70, 255})
	}
	phase := a.elapsed.Seconds() * 2
	lastX, lastY := plot.Min.X, plot.Min.Y+plot.Dy()/2
	for x := plot.Min.X + 1; x < plot.Max.X; x++ {
		t := float64(x-plot.Min.X) / float64(max(1, plot.Dx()))
		y := plot.Min.Y + plot.Dy()/2 + int(math.Sin(t*18+phase)*float64(plot.Dy())*.22+math.Sin(t*47-phase*.3)*float64(plot.Dy())*.06)
		line(a.image, lastX, lastY, x, y, color.RGBA{35, 230, 245, 255})
		lastX, lastY = x, y
	}
	button := image.Rect(28, h-54, 174, h-24)
	draw.Draw(a.image, button, &image.Uniform{C: color.RGBA{8, 65, 83, 255}}, image.Point{}, draw.Src)
	strokeRect(a.image, button, color.RGBA{25, 205, 235, 255}, 1)
	state := "PAUSE STREAM"
	if !a.running {
		state = "RESUME STREAM"
	}
	label(a.image, 42, h-34, state, color.RGBA{120, 245, 255, 255})
	focus := "REMOTE"
	if a.focused {
		focus = "FOCUSED"
	}
	label(a.image, w-130, h-34, focus, color.RGBA{242, 142, 45, 255})
	region := damage.Intersect(bounds)
	a.pending = []nativeapp.TextureUpdate{{
		ID: textureID, Revision: a.revision, Width: w, Height: h,
		Rect:   nativeapp.Rect{X: region.Min.X, Y: region.Min.Y, Width: region.Dx(), Height: region.Dy()},
		Pixels: rgbaRegion(a.image, region),
	}}
}

func rgbaRegion(source *image.RGBA, region image.Rectangle) []byte {
	result := make([]byte, region.Dx()*region.Dy()*4)
	for y := 0; y < region.Dy(); y++ {
		copy(result[y*region.Dx()*4:(y+1)*region.Dx()*4], source.Pix[(region.Min.Y+y)*source.Stride+region.Min.X*4:(region.Min.Y+y)*source.Stride+region.Max.X*4])
	}
	return result
}

func label(target *image.RGBA, x, y int, text string, ink color.Color) {
	d := font.Drawer{Dst: target, Src: image.NewUniform(ink), Face: basicfont.Face7x13, Dot: fixedPoint(x, y)}
	d.DrawString(text)
}

func fixedPoint(x, y int) fixed.Point26_6 { return fixed.P(x, y) }

func strokeRect(target *image.RGBA, bounds image.Rectangle, ink color.RGBA, width int) {
	for i := 0; i < width; i++ {
		line(target, bounds.Min.X+i, bounds.Min.Y+i, bounds.Max.X-1-i, bounds.Min.Y+i, ink)
		line(target, bounds.Min.X+i, bounds.Max.Y-1-i, bounds.Max.X-1-i, bounds.Max.Y-1-i, ink)
		line(target, bounds.Min.X+i, bounds.Min.Y+i, bounds.Min.X+i, bounds.Max.Y-1-i, ink)
		line(target, bounds.Max.X-1-i, bounds.Min.Y+i, bounds.Max.X-1-i, bounds.Max.Y-1-i, ink)
	}
}

func line(target *image.RGBA, x0, y0, x1, y1 int, ink color.RGBA) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := -1, -1
	if x0 < x1 {
		sx = 1
	}
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		if image.Pt(x0, y0).In(target.Bounds()) {
			target.SetRGBA(x0, y0, ink)
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func instrumentMesh() nativeapp.MeshResource {
	return nativeapp.MeshResource{
		ID:       meshID,
		Vertices: []nativeapp.Vec3{{X: 0, Y: .7, Z: 0}, {X: -.55, Y: 0, Z: -.35}, {X: .55, Y: 0, Z: -.35}, {X: 0, Y: 0, Z: .6}, {X: 0, Y: -.7, Z: 0}},
		Indices:  []uint32{0, 1, 2, 0, 2, 3, 0, 3, 1, 4, 2, 1, 4, 3, 2, 4, 1, 3},
	}
}

func instrumentGlassMesh() nativeapp.MeshResource {
	vertices := []nativeapp.Vec3{{X: 0, Y: .7}, {X: -.55, Z: -.35}, {X: .55, Z: -.35}, {Z: .6}, {Y: -.7}}
	normals := make([]nativeapp.Vec3, len(vertices))
	for i, vertex := range vertices {
		length := float32(math.Sqrt(float64(vertex.X*vertex.X + vertex.Y*vertex.Y + vertex.Z*vertex.Z)))
		if length > 0 {
			normals[i] = nativeapp.Vec3{X: vertex.X / length, Y: vertex.Y / length, Z: vertex.Z / length}
		}
	}
	return nativeapp.MeshResource{
		ID: glassID, Vertices: vertices, Normals: normals,
		Indices: []uint32{0, 1, 2, 0, 2, 3, 0, 3, 1, 4, 2, 1, 4, 3, 2, 4, 1, 3},
	}
}

func modelMatrix(angle float32) [16]float32 {
	c, s := float32(math.Cos(float64(angle))), float32(math.Sin(float64(angle)))
	const scale = float32(.22)
	return [16]float32{c * scale, 0, -s * scale, 0, 0, scale, 0, 0, s * scale, 0, c * scale, 0, .3, .22, .18, 1}
}

func scaleMatrix(scale float32) [16]float32 {
	return [16]float32{scale, 0, 0, 0, 0, scale, 0, 0, 0, 0, scale, 0, 0, 0, 0, 1}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func main() {
	if err := nativeapp.Serve(context.Background(), &instrument{}, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "native instrument:", err)
		os.Exit(1)
	}
}
