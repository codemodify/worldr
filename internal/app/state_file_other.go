//go:build !linux

package app

import (
	"fmt"
	"os"
)

func openStateFile(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("state must be a regular file")
	}
	return os.Open(path)
}
