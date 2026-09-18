// Package workspace provides the spatial desktop and the optional AXIAL study.
// The study’s geometry and signals are illustrations, not a physics solver.
package workspace

import (
	"math"
	"time"

	"github.com/codemodify/worldr/internal/presentation"
	"github.com/codemodify/worldr/internal/render"
)

const duration = 30.0

type component struct {
	name, description, material, dimension, detail string
	color                                          uint32
	appearance                                     render.Material
}

var components = [...]component{
	{"Outer housing", "A cutaway reveals the moving assembly.", "Ceramic composite", "3.90", "300° cutaway / 1.55 span", 0x9cacc0,
		render.Material{Specular: .42, Roughness: .46, RimStrength: .12, RimColor: [3]float32{.28, .68, .94}}},
	{"Rotor core", "Eighteen blades around a common axis.", "Titanium alloy", "3.12", "18 blades / 0.68 hub", 0x8dbfda,
		render.Material{Specular: .9, Roughness: .26, Metallic: .8, RimStrength: .18, RimColor: [3]float32{.32, .82, 1}}},
	{"Drive shaft", "One continuous axis connects the system.", "Anodized steel", "5.40", "0.48 diameter / two collars", 0x718798,
		render.Material{Specular: .86, Roughness: .19, Metallic: .92, RimStrength: .14, RimColor: [3]float32{.32, .68, 1}}},
}

// State is a read-only snapshot suitable for an inspector or automation.
type State struct {
	Time                                                   float64
	Playing, Exploded, Focused                             bool
	Selected                                               int
	Explosion, Zoom                                        float32
	Yaw, Pitch                                             float32
	Presentation                                           presentation.Mode
	ReducedMotion                                          bool
	PanelBehind                                            bool
	ApplicationBehind, ApplicationReading, ApplicationWide bool
}

type model struct {
	clock                                                  float64
	playing, exploded, focused                             bool
	selected                                               int
	explosion                                              float32
	yaw, pitch, zoom                                       float32
	cameraX, cameraY, cameraDepth                          float32
	presentation                                           presentation.Mode
	reducedMotion                                          bool
	panelBehind                                            bool
	applicationBehind, applicationReading, applicationWide bool
	applicationState                                       ApplicationViewState
}

func initialModel() model {
	return model{clock: 6, playing: true, selected: 1, yaw: 0.93, pitch: 0.32, presentation: presentation.Cinematic, panelBehind: true}
}

func (m *model) update(dt time.Duration) {
	seconds := dt.Seconds()
	if seconds <= 0 {
		return
	}
	if m.playing {
		m.clock = math.Mod(m.clock+seconds, duration)
	}
	target := float32(0)
	if m.exploded {
		target = 1
	}
	if m.reducedMotion {
		m.explosion = target
	} else {
		m.explosion += (target - m.explosion) * float32(1-math.Exp(-seconds*7))
	}
	if abs(m.explosion-target) < 0.0001 {
		m.explosion = target
	}
}

func signal(selected int, t float64) float64 {
	phase := float64(selected) * 0.75
	return 0.48 + 0.18*math.Sin(t*0.68+phase) + 0.055*math.Sin(t*2.2+phase*0.4) + 0.022*math.Cos(t*4.8)
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
