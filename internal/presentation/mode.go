// Package presentation defines presentation preferences shared by native
// experiences. A mode changes visual treatment, not an experience's capabilities.
package presentation

import (
	"encoding/json"
	"fmt"
)

type Mode string

const (
	// Cinematic keeps expressive spatial framing visible during focused work.
	Cinematic Mode = "cinematic"
	// Adaptive quiets surrounding detail while the user focuses on an object.
	Adaptive Mode = "adaptive"
)

func (m Mode) Valid() bool { return m == Cinematic || m == Adaptive }

// UnmarshalJSON rejects explicit null as well as unsupported modes. Missing
// fields do not call this method, so callers can supply a migration default.
func (m *Mode) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode presentation mode: %w", err)
	}
	mode := Mode(value)
	if !mode.Valid() {
		return fmt.Errorf("unknown presentation mode %q", mode)
	}
	*m = mode
	return nil
}

func (m Mode) Next() Mode {
	if m == Cinematic {
		return Adaptive
	}
	return Cinematic
}

func (m Mode) Label() string {
	switch m {
	case Cinematic:
		return "Cinematic"
	case Adaptive:
		return "Adaptive"
	default:
		return "Unknown"
	}
}
