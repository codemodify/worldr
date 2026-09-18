// Package seat owns direct-display session access. Nested sessions do not open
// a seat. The host must stop presentation and close device leases before
// acknowledging a disable event, then reopen them after the next enable event.
package seat

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrClosed   = errors.New("seat is closed")
	ErrInactive = errors.New("seat is inactive")
)

type Event uint8

const (
	Enabled Event = iota + 1
	Disabled
)

type backend interface {
	dispatch() (Event, error)
	name() string
	openDevice(string) (int, int, error)
	closeDevice(int) error
	disable() error
	switchSession(int) error
	close() error
	newInput() (inputBackend, error)
}

// Session and all its leases belong to one host goroutine.
type Session struct {
	driver                  backend
	active, pending, closed bool
	devices                 map[*Device]bool
	input                   *Input
	closeErr                error
}

func newSession(driver backend) *Session {
	return &Session{driver: driver, devices: make(map[*Device]bool)}
}
func (s *Session) Name() string {
	if s == nil || s.closed {
		return ""
	}
	return s.driver.name()
}
func (s *Session) Active() bool { return s != nil && !s.closed && s.active && !s.pending }

// Dispatch performs nonblocking work and delivers at most one lifecycle edge.
// This preserves disable/ack/enable ordering even if the backend queued both.
func (s *Session) Dispatch() ([]Event, error) {
	if s == nil || s.closed {
		return nil, ErrClosed
	}
	if s.pending {
		return nil, errors.New("seat disable must be acknowledged before dispatch")
	}
	event, err := s.driver.dispatch()
	if err != nil {
		return nil, err
	}
	switch event {
	case Enabled:
		if s.active {
			return nil, nil
		}
		s.active = true
	case Disabled:
		s.active = false
		s.pending = true
	default:
		return nil, nil
	}
	return []Event{event}, nil
}
func (s *Session) OpenDevice(path string) (*Device, error) {
	if s == nil || s.closed {
		return nil, ErrClosed
	}
	if !s.Active() {
		return nil, ErrInactive
	}
	if !filepath.IsAbs(path) || len(path) > 4096 || strings.IndexByte(path, 0) >= 0 {
		return nil, errors.New("seat device requires an absolute path")
	}
	id, fd, err := s.driver.openDevice(path)
	if err != nil {
		return nil, err
	}
	if fd < 0 {
		return nil, errors.New("seat returned an invalid device descriptor")
	}
	d := &Device{session: s, id: id, file: os.NewFile(uintptr(fd), path)}
	s.devices[d] = true
	return d, nil
}

// Device.FD is borrowed. Native consumers may duplicate it, but must close
// those duplicates before closing this lease or acknowledging seat disable.
type Device struct {
	session  *Session
	id       int
	file     *os.File
	closed   bool
	closeErr error
}

func (d *Device) FD() int {
	if d == nil || d.closed || d.file == nil {
		return -1
	}
	return int(d.file.Fd())
}
func (d *Device) Close() error {
	if d == nil {
		return nil
	}
	if d.closed {
		return d.closeErr
	}
	d.closed = true
	// libseat's backend close releases session ownership, not our descriptor.
	d.closeErr = d.session.driver.closeDevice(d.id)
	d.closeErr = errors.Join(d.closeErr, d.file.Close())
	d.file = nil
	delete(d.session.devices, d)
	return d.closeErr
}
func (s *Session) AcknowledgeDisable() error {
	if s == nil || s.closed {
		return ErrClosed
	}
	if !s.pending {
		return nil
	}
	if len(s.devices) > 0 || s.input != nil {
		return fmt.Errorf("seat disable has %d open device leases or live input; release them before acknowledging", len(s.devices))
	}
	if err := s.driver.disable(); err != nil {
		return err
	}
	s.pending = false
	return nil
}
func (s *Session) SwitchSession(vt int) error {
	if s == nil || s.closed {
		return ErrClosed
	}
	if !s.Active() {
		return ErrInactive
	}
	if vt < 1 || vt > 63 {
		return errors.New("VT number must be between 1 and 63")
	}
	return s.driver.switchSession(vt)
}
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	if s.closed {
		return s.closeErr
	}
	if s.input != nil {
		s.closeErr = errors.Join(s.closeErr, s.input.Close())
	}
	for d := range s.devices {
		s.closeErr = errors.Join(s.closeErr, d.Close())
	}
	if s.pending {
		s.closeErr = errors.Join(s.closeErr, s.AcknowledgeDisable())
	}
	s.closeErr = errors.Join(s.closeErr, s.driver.close())
	s.closed = true
	s.active = false
	return s.closeErr
}

type InputKind uint8

const (
	Motion InputKind = iota + 1
	Absolute
	Button
	Key
	Scroll
	Cancel
)

// Absolute coordinates are normalized to [0,1]. Motion is in logical pixels,
// scroll is ten units per wheel detent or continuous libinput scroll units.
type InputEvent struct {
	Kind       InputKind
	X, Y       float64
	Code, Time uint32
	Pressed    bool
}
type inputBackend interface {
	poll() ([]InputEvent, error)
	close() error
}
type Input struct {
	session  *Session
	driver   inputBackend
	closed   bool
	closeErr error
}

func (s *Session) OpenInput() (*Input, error) {
	if s == nil || s.closed {
		return nil, ErrClosed
	}
	if !s.Active() {
		return nil, ErrInactive
	}
	if s.input != nil {
		return nil, errors.New("seat already has an input context")
	}
	driver, err := s.driver.newInput()
	if err != nil {
		return nil, err
	}
	i := &Input{session: s, driver: driver}
	s.input = i
	return i, nil
}
func (i *Input) Poll() ([]InputEvent, error) {
	if i == nil || i.closed {
		return nil, ErrClosed
	}
	if !i.session.Active() {
		return nil, ErrInactive
	}
	return i.driver.poll()
}
func (i *Input) Close() error {
	if i == nil {
		return nil
	}
	if i.closed {
		return i.closeErr
	}
	i.closed = true
	i.closeErr = i.driver.close()
	i.session.input = nil
	return i.closeErr
}
