//go:build !linux

package projectapp

import "fmt"

func openReader(string) (projectReader, string, error) {
	return nil, "", fmt.Errorf("native project browsing currently requires Linux")
}

func openSessionReader(root string) (projectReader, string, error) {
	return openReader(root)
}
