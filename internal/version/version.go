// Package version reports the scaffold / release identifier.
package version

// Phase is the current development phase. Phase 0 is docs + module layout only.
const Phase = "0"

// Version is the placeholder semver for this tree.
const Version = "0.0.0-phase0"

// String returns the version identifier.
func String() string {
	return Version
}
