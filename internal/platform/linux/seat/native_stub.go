//go:build !linux || !cgo

package seat

import "errors"

func Open() (*Session, error) {
	return nil, errors.New("direct seats require Linux, cgo, libseat, libinput and libudev")
}
