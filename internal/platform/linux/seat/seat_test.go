package seat

import (
	"errors"
	"os"
	"reflect"
	"runtime"
	"testing"
)

type fakeBackend struct {
	events     []Event
	fd         int
	log        []string
	releaseErr error
	input      *fakeInput
}

func (b *fakeBackend) dispatch() (Event, error) {
	if len(b.events) == 0 {
		return 0, nil
	}
	e := b.events[0]
	b.events = b.events[1:]
	return e, nil
}
func (*fakeBackend) name() string { return "seat-fixture" }
func (b *fakeBackend) openDevice(string) (int, int, error) {
	b.log = append(b.log, "open device")
	return 7, b.fd, nil
}
func (b *fakeBackend) closeDevice(int) error {
	b.log = append(b.log, "release device")
	return b.releaseErr
}
func (b *fakeBackend) disable() error          { b.log = append(b.log, "ack disable"); return nil }
func (b *fakeBackend) switchSession(int) error { b.log = append(b.log, "switch"); return nil }
func (b *fakeBackend) close() error            { b.log = append(b.log, "close seat"); return nil }
func (b *fakeBackend) newInput() (inputBackend, error) {
	b.log = append(b.log, "open input")
	b.input = &fakeInput{backend: b}
	return b.input, nil
}

type fakeInput struct{ backend *fakeBackend }

func (*fakeInput) poll() ([]InputEvent, error) {
	return []InputEvent{{Kind: Key, Code: 30, Pressed: true}}, nil
}
func (i *fakeInput) close() error { i.backend.log = append(i.backend.log, "close input"); return nil }

func TestDisableCannotAcknowledgeLiveDevicesOrInput(t *testing.T) {
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	// The backend transfers this descriptor; Device.Close owns the only close.
	runtime.SetFinalizer(file, nil)
	b := &fakeBackend{events: []Event{Enabled, Disabled, Enabled}, fd: int(file.Fd())}
	s := newSession(b)
	if _, err := s.OpenDevice("/dev/dri/card0"); !errors.Is(err, ErrInactive) {
		t.Fatalf("inactive open: %v", err)
	}
	events, err := s.Dispatch()
	if err != nil || !reflect.DeepEqual(events, []Event{Enabled}) || !s.Active() {
		t.Fatalf("initial enable %v %v", events, err)
	}
	d, err := s.OpenDevice("/dev/dri/card0")
	if err != nil {
		t.Fatal(err)
	}
	i, err := s.OpenInput()
	if err != nil {
		t.Fatal(err)
	}
	events, err = s.Dispatch()
	if err != nil || !reflect.DeepEqual(events, []Event{Disabled}) || s.Active() {
		t.Fatalf("disable %v %v", events, err)
	}
	if _, err := i.Poll(); !errors.Is(err, ErrInactive) {
		t.Fatal("inactive seat read input")
	}
	if err := s.AcknowledgeDisable(); err == nil {
		t.Fatal("acknowledged with live DRM/input")
	}
	if _, err := s.Dispatch(); err == nil {
		t.Fatal("delivered enable before disable acknowledgment")
	}
	if err := i.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.AcknowledgeDisable(); err == nil {
		t.Fatal("acknowledged before external device closed")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if d.FD() != -1 {
		t.Fatal("released device still lends descriptor")
	}
	if _, err := file.Stat(); err == nil {
		t.Fatal("device did not close its descriptor")
	}
	if err := s.AcknowledgeDisable(); err != nil {
		t.Fatal(err)
	}
	events, err = s.Dispatch()
	if err != nil || !reflect.DeepEqual(events, []Event{Enabled}) || !s.Active() {
		t.Fatalf("resume %v %v", events, err)
	}
	if err := s.SwitchSession(0); err == nil {
		t.Fatal("invalid VT accepted")
	}
	if err := s.SwitchSession(3); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	want := []string{"open device", "open input", "close input", "release device", "ack disable", "switch", "close seat"}
	if !reflect.DeepEqual(b.log, want) {
		t.Fatalf("lifecycle order %v want %v", b.log, want)
	}
	if s.Active() || s.Name() != "" {
		t.Fatal("closed session retained state")
	}
}

func TestCloseContinuesAfterDeviceReleaseFailure(t *testing.T) {
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("release fixture")
	runtime.SetFinalizer(file, nil)
	b := &fakeBackend{events: []Event{Enabled, Disabled}, fd: int(file.Fd()), releaseErr: want}
	s := newSession(b)
	s.Dispatch()
	d, err := s.OpenDevice("/dev/input/event0")
	if err != nil {
		t.Fatal(err)
	}
	i, err := s.OpenInput()
	if err != nil {
		t.Fatal(err)
	}
	s.Dispatch()
	if err := s.Close(); !errors.Is(err, want) {
		t.Fatalf("release error lost: %v", err)
	}
	if _, err := file.Stat(); err == nil || d.FD() != -1 || !i.closed {
		t.Fatal("failure left owned resources live")
	}
	if got := b.log[len(b.log)-2:]; !reflect.DeepEqual(got, []string{"ack disable", "close seat"}) {
		t.Fatalf("failed release skipped final shutdown: %v", b.log)
	}
	if err := d.Close(); !errors.Is(err, want) {
		t.Fatal("device close did not retain its result")
	}
	if _, err := s.Dispatch(); !errors.Is(err, ErrClosed) {
		t.Fatal("closed dispatch succeeded")
	}
}

func TestDuplicateEnableAndInputContextOwnership(t *testing.T) {
	b := &fakeBackend{events: []Event{Enabled, Enabled}}
	s := newSession(b)
	defer s.Close()
	s.Dispatch()
	if events, err := s.Dispatch(); err != nil || len(events) != 0 {
		t.Fatal("duplicate enable replayed renderer startup")
	}
	i, err := s.OpenInput()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenInput(); err == nil {
		t.Fatal("duplicate libinput context accepted")
	}
	if events, err := i.Poll(); err != nil || len(events) != 1 || events[0].Code != 30 {
		t.Fatal("input events lost")
	}
	i.Close()
	i.Close()
	if _, err := s.OpenInput(); err != nil {
		t.Fatal("closed input prevented reopening")
	}
}
