//go:build !linux || !cgo

package terminal

import (
	"fmt"
	"github.com/codemodify/worldr/internal/experience"
)

type Terminal struct{}

func Open(Options) (*Terminal, error) {
	return nil, fmt.Errorf("native terminal requires Linux, CGO, libvterm >= 0.3 and xkbcommon")
}
func (*Terminal) Poll() (Snapshot, error)           { return Snapshot{}, ErrClosed }
func (*Terminal) Resize(int, int) error             { return ErrClosed }
func (*Terminal) Input(experience.Event) error      { return ErrClosed }
func (*Terminal) Focus(bool)                        {}
func (*Terminal) Paste(string) error                { return ErrClosed }
func (*Terminal) Scroll(int)                        {}
func (*Terminal) Close() error                      { return nil }
func (*Terminal) WorkingDirectory() (string, error) { return "", ErrClosed }
func (*Terminal) History() (History, error)         { return History{}, ErrClosed }
func (*Terminal) ScrollToLine(uint64) bool          { return false }
