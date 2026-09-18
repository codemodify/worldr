//go:build !linux

package app

import "os"

func lockSession(path string) (*os.File, error) { return nil, nil }
