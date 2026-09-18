package researchapp

import (
	"fmt"
	"math"
	"strings"

	"github.com/codemodify/worldr/internal/resourcepath"
)

const MaxDashboards = 8

type ViewState struct {
	X        int     `json:"x"`
	Y        int     `json:"y"`
	Z        int     `json:"z"`
	Selected int     `json:"selected"`
	Yaw      float32 `json:"yaw"`
	Pitch    float32 `json:"pitch"`
	Zoom     float32 `json:"zoom"`
	Mode     string  `json:"mode"`
}

type SessionState struct {
	Key    string    `json:"key"`
	Source string    `json:"source"`
	View   ViewState `json:"view"`
}

func slotKey(slot int) string {
	if slot == 0 {
		return "native:research-workbench"
	}
	return fmt.Sprintf("native:research-workbench-%d", slot+1)
}

func keySlot(key string) (int, error) {
	for slot := 0; slot < MaxDashboards; slot++ {
		if slotKey(slot) == key {
			return slot, nil
		}
	}
	return -1, fmt.Errorf("invalid research dashboard key %q", key)
}

func defaultView() ViewState {
	return ViewState{X: 0, Y: 0, Z: 0, Selected: 0, Yaw: -.45, Pitch: .38, Zoom: 1, Mode: "line"}
}

func (s SessionState) Validate() error {
	if _, err := keySlot(s.Key); err != nil {
		return err
	}
	if err := resourcepath.Validate(s.Source); err != nil {
		return fmt.Errorf("research dataset: %w", err)
	}
	if s.View.X < 0 || s.View.X >= maxColumns || s.View.Y < 0 || s.View.Y >= maxColumns || s.View.Z < 0 || s.View.Z >= maxColumns || s.View.Selected < 0 || s.View.Selected >= maxRows {
		return fmt.Errorf("research view indices are out of bounds")
	}
	for _, value := range []float32{s.View.Yaw, s.View.Pitch, s.View.Zoom} {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("research view values must be finite")
		}
	}
	if s.View.Zoom < .2 || s.View.Zoom > 3 {
		return fmt.Errorf("research zoom must be within 0.2..3")
	}
	if mode := strings.ToLower(s.View.Mode); mode != "line" && mode != "scatter" {
		return fmt.Errorf("research chart mode must be line or scatter")
	}
	return nil
}
