//go:build !linux || !cgo

package media

import "os"

type Player struct{}

func New(Options) (*Player, error)                         { return nil, ErrUnavailable }
func (*Player) Load(*os.File) error                        { return ErrUnavailable }
func (*Player) Poll() (State, error)                       { return State{}, ErrUnavailable }
func (*Player) Render([]byte, int, int, int) (bool, error) { return false, ErrUnavailable }
func (*Player) Pause(bool) error                           { return ErrUnavailable }
func (*Player) Seek(float64) error                         { return ErrUnavailable }
func (*Player) SetVolume(float64) error                    { return ErrUnavailable }
func (*Player) SetMute(bool) error                         { return ErrUnavailable }
func (*Player) Close() error                               { return nil }
