// Package version reports the native scene engine development version.
package version

// Phase is the current development phase.
const Phase = "native-scene"

// Version identifies the architecture reset; this is not a stable desktop release.
const Version = "0.10.0-dev"

// String returns the version identifier.
func String() string {
	return Version
}
