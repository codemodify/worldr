//go:build !linux

package nativeapps

import "os"

func openFallbackFont(path string) (*os.File, error) { return os.Open(path) }
