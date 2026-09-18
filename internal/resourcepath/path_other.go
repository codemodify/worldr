//go:build !linux

package resourcepath

import (
	"fmt"
	"os"
	"path/filepath"
)

func descriptorPath(file *os.File) (string, error) {
	path, err := filepath.Abs(file.Name())
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

func openResource(path string, directory bool) (*os.File, error) {
	return nil, fmt.Errorf("session resource reopening requires Linux")
}
