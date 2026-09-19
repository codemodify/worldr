// Package presentation defines the persisted visual style identifier. Worldr
// now runs Cinematic exclusively; the decoder retains its former value only to
// migrate existing documents.
package presentation

import (
	"encoding/json"
	"fmt"
)

type Mode string

const (
	// Cinematic is worldr's permanent expressive spatial presentation.
	Cinematic Mode = "cinematic"
	// legacyAdaptive is accepted only while decoding documents written by the
	// former selectable-presentation prototype.
	legacyAdaptive Mode = "adaptive"
)

func (m Mode) Valid() bool { return m == Cinematic }

// UnmarshalJSON rejects explicit null as well as unsupported modes. Missing
// fields do not call this method, so callers can supply a migration default.
func (m *Mode) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode presentation mode: %w", err)
	}
	mode := Mode(value)
	if mode != Cinematic && mode != legacyAdaptive {
		return fmt.Errorf("unknown presentation mode %q", mode)
	}
	*m = Cinematic
	return nil
}

func (m Mode) Label() string {
	switch m {
	case Cinematic:
		return "Cinematic"
	default:
		return "Unknown"
	}
}
