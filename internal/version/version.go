// Package version reports the scaffold / release identifier.
package version

// Phase is the current development phase.
const Phase = "5"

// Version is the placeholder semver for this tree.
const Version = "0.9.7-dev"

// String returns the version identifier.
func String() string {
	return Version
}
