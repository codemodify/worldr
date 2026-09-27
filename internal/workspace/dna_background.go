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
	w.ambientCat, err = newAmbientCat(w.backgroundScene)
	if err != nil {
		return err
	}
	return nil
}

func (w *Workspace) updateBackground(dt time.Duration) {
	// Hidden ambient actors must not collide with the authored desktop's
	// windows. Drop old visual events and contact baselines even on a zero-time
	// update, so returning to the ambient desktop starts with fresh contacts.
	if w.skinBackdropVisible() {
		w.energyWallImpacts = w.energyWallImpacts[:0]
		w.resetAmbientCatCollisions()
		return
	}
	if dt <= 0 {
		return
	}
	// Integer elapsed time makes the pose independent of frame rate. Reduce
	// the delta first so even a very long interruption cannot overflow it.
	w.backgroundPhase = (w.backgroundPhase + dt%dnaRotationPeriod) % dnaRotationPeriod
	w.ambientCat.update(dt)
	w.updateAmbientCatCollisions()
	w.updateEnergyNet(dt)
}

// roomPlacement maps a landmark's local axes onto the desktop. Local X runs
// along the window plane, local Y stands up in the view, and local Z points
// toward the rear wall. The result turns with the room because the same camera
// draws the windows.
func roomPlacement(center scene.Vec3) scene.Mat4 {
	right, up, normal := applicationBasis()
	return scene.Mat4{
		right.X, right.Y, right.Z, 0,
		up.X, up.Y, up.Z, 0,
		normal.X, normal.Y, normal.Z, 0,
		center.X, center.Y, center.Z, 1,
	}
}

func dnaLandmarkCenter() scene.Vec3 {
	right, _, normal := applicationBasis()
	// Left of the working plane and in front of the rear weave, so the helix is
	// a place you can turn toward rather than a picture stuck to the glass.
	return right.Mul(-4.4).Add(normal.Mul(-1.5))
}

func catLandmarkCenter() scene.Vec3 {
	_, _, normal := applicationBasis()
	return normal.Mul(-0.6)
}

func (w *Workspace) ambientQuiet() bool {
	return w.desktop && (w.m.applicationState.Reading || w.m.applicationReading)
}

func (w *Workspace) drawBackground() {
	if w.drawSkinBackdrop() {
		return
	}
	quiet := w.ambientQuiet()
	if !quiet {
		w.drawEnergyNet()
	}
	if w.backgroundScene == nil {
		return
	}
	strand := w.backgroundScene.Node(w.dnaNode)
	strand.Hidden = !w.environment.DNA
	alpha := float32(.28)
	glow := [3]float32{.008, .04, .06}
	if quiet {
		// Read keeps a faint landmark so the page still has a room behind it.
		alpha = .06
		glow = [3]float32{.002, .01, .016}
	}
	strand.Color = scene.ColorHex(0x90d8ed, alpha)
	strand.Glow = glow
	angle := float32(2 * math.Pi * float64(w.backgroundPhase) / float64(dnaRotationPeriod))
	strand.Transform = roomPlacement(dnaLandmarkCenter()).Mul(scene.RotateY(angle)).Mul(scene.Scale(.38, .38, .38))
	local := scene.Identity()
	var catRoot *scene.Node
	if w.ambientCat != nil {
		w.ambientCat.setVisible(w.environment.Cat && !quiet)
		catRoot = w.backgroundScene.Node(w.ambientCat.root)
		if catRoot != nil {
			w.ambientCat.syncPose()
			local = catRoot.Transform
			catRoot.Transform = roomPlacement(catLandmarkCenter()).Mul(local)
		}
	}
	// This pass is before workspace content and is never picked. It uses the
	// desktop camera, so looking around carries the landmarks with the windows.
	w.backgroundScene.Draw(w.canvas, w.camera, w.viewport)
	if catRoot != nil {
		catRoot.Transform = local
	}
	if !quiet {
		w.drawBackgroundEyes()
	}
}
