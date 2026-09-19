package workspace

import (
	"math"
	"time"

	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

const dnaRotationPeriod = 30 * time.Second

func (w *Workspace) initializeBackground() error {
	mesh, err := dnaMesh()
	if err != nil {
		return err
	}
	w.backgroundScene = scene.NewScene()
	w.dnaNode = w.backgroundScene.Add(0, scene.Node{
		Mesh: mesh, Unpickable: true,
		Material: render.Material{Specular: .38, Roughness: .4, RimStrength: .2, RimColor: [3]float32{.08, .55, .7}},
	})
	return nil
}

func (w *Workspace) updateBackground(dt time.Duration) {
	if w.backgroundScene == nil || w.m.reducedMotion || dt <= 0 {
		return
	}
	// Integer elapsed time makes the pose independent of frame rate. Reduce
	// the delta first so even a very long interruption cannot overflow it.
	w.backgroundPhase = (w.backgroundPhase + dt%dnaRotationPeriod) % dnaRotationPeriod
}

func (w *Workspace) drawBackground() {
	if w.backgroundScene == nil {
		return
	}
	strand := w.backgroundScene.Node(w.dnaNode)
	strength := w.presentationBlend
	strand.Hidden = strength <= 0
	strand.Color = scene.ColorHex(0x90d8ed, .57*strength)
	strand.Glow = [3]float32{.015 * strength, .085 * strength, .12 * strength}
	angle := float32(2 * math.Pi * float64(w.backgroundPhase) / float64(dnaRotationPeriod))
	// Keep the helix's long axis vertical at the center of the viewport. The
	// camera-independent framing keeps it a backdrop as windows move in depth or
	// enter Read mode; rotating around its own axis never orbits it around UI.
	strand.Transform = scene.RotateY(angle)
	camera := scene.Camera{Eye: scene.Vec3{Z: 15.5}, Up: scene.Vec3{Y: 1}, FOV: .69, Near: .1, Far: 40}
	// Ordered camera passes clear depth separately. This pass is always before
	// workspace content, so even a window placed far back covers the backdrop. The
	// background scene is never consulted for picking or pointer capture.
	w.backgroundScene.Draw(w.canvas, camera, w.viewport)
}
