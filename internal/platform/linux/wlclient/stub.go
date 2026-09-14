//go:build !linux

package wlclient

import "fmt"

type Window struct{}

func Open(title string, w, h int, fullscreen bool) (*Window, error) {
	return nil, fmt.Errorf("wayland-client backend is linux-only")
}

func (w *Window) Size() (int, int, int) { return 0, 0, 0 }
func (w *Window) Present([]byte, int) error {
	return fmt.Errorf("wayland-client backend is linux-only")
}
func (w *Window) Close()           {}
func (w *Window) TakeInput() Input { return Input{} }
func (w *Window) BoundVersions() (uint32, uint32, uint32) {
	return 0, 0, 0
}
