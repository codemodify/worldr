//go:build !linux || !cgo

package nativeapps

func newSystemFontMatcher() terminalFontMatcher { return nil }
