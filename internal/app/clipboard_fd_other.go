//go:build !linux

package app

import "fmt"

func duplicateClipboardFD(fd int) (int, error) {
	return -1, fmt.Errorf("clipboard relay requires Linux")
}
