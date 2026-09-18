package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

type lifecycleHubProvider struct {
	hubProvider
	name                  string
	log                   *[]string
	pollErr, closeErr     error
	polls, closes, drains int
	surfaceReads          int
	focusCalls            int
	launches, cursors     int
	retired, closeRetired []uint64
	onClose               func()
}

func (p *lifecycleHubProvider) Poll() error {
	p.polls++
	if p.log != nil {
		*p.log = append(*p.log, "poll "+p.name)
	}
	return p.pollErr
}

func (p *lifecycleHubProvider) Close() error {
	p.closes++
	if p.log != nil {
		*p.log = append(*p.log, "close "+p.name)
	}
	p.retired = append(p.retired, p.closeRetired...)
	if p.onClose != nil {
		p.onClose()
	}
	return p.closeErr
}

func (p *lifecycleHubProvider) RetiredTextures() []uint64 {
	p.drains++
	retired := p.retired
	p.retired = nil
	return retired
}

func (p *lifecycleHubProvider) Surfaces() []experience.ApplicationSurface {
	p.surfaceReads++
	return p.hubProvider.Surfaces()
}

func (p *lifecycleHubProvider) Focus(id uint64) {
	p.focusCalls++
	p.hubProvider.Focus(id)
}

func (p *lifecycleHubProvider) LaunchApplication(string) (string, error) {
	p.launches++
	return "launched", nil
}

func (p *lifecycleHubProvider) ApplicationCursor(uint64) (experience.ApplicationCursor, bool) {
	p.cursors++
	return experience.ApplicationCursor{Hidden: true}, true
}

func TestApplicationHubPollKeepsSiblingProvidersMovingAfterError(t *testing.T) {
	firstFailure, secondFailure := errors.New("first failed"), errors.New("second failed")
	var log []string
	first := &lifecycleHubProvider{name: "first", log: &log, pollErr: firstFailure}
	plain := &hubProvider{surfaces: []experience.ApplicationSurface{{ID: 9}}}
	second := &lifecycleHubProvider{name: "second", log: &log, pollErr: secondFailure}
	h := newApplicationHub(first, plain, second)
	for turn := 1; turn <= 2; turn++ {
		err := h.Poll()
		if !errors.Is(err, firstFailure) || errors.Is(err, secondFailure) || !strings.Contains(err.Error(), "provider 1") {
			t.Fatalf("poll lost first provider context: %v", err)
		}
		if first.polls != turn || second.polls != turn {
			t.Fatalf("poll did not advance every provider exactly once: first=%d second=%d", first.polls, second.polls)
		}
	}
	if want := []string{"poll first", "poll second", "poll first", "poll second"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("poll order = %v, want %v", log, want)
	}
	if surfaces := h.Surfaces(); len(surfaces) != 1 {
		t.Fatalf("provider without lifecycle methods lost its surface: %v", surfaces)
	}
	first.pollErr, second.pollErr = nil, nil
	if err := h.Poll(); err != nil {
		t.Fatalf("recovered providers still reported an old error: %v", err)
	}
}

func TestApplicationHubCloseReversesRegistrationAndJoinsFailures(t *testing.T) {
	firstFailure, lastFailure := errors.New("first teardown failed"), errors.New("last teardown failed")
	var log []string
	first := &lifecycleHubProvider{name: "first", log: &log, closeErr: firstFailure}
	middle := &lifecycleHubProvider{name: "middle", log: &log}
	last := &lifecycleHubProvider{name: "last", log: &log, closeErr: lastFailure}
	h := newApplicationHub(first, &hubProvider{}, middle, last)
	err := h.Close()
	if !errors.Is(err, firstFailure) || !errors.Is(err, lastFailure) {
		t.Fatalf("close dropped a provider failure: %v", err)
	}
	if !strings.Contains(err.Error(), "provider 1") || !strings.Contains(err.Error(), "provider 4") {
		t.Fatalf("close lost provider context: %v", err)
	}
	if want := []string{"close last", "close middle", "close first"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("close order = %v, want %v", log, want)
	}
	if again := h.Close(); again != err {
		t.Fatalf("repeated close changed its result: first=%v again=%v", err, again)
	}
	if first.closes != 1 || middle.closes != 1 || last.closes != 1 {
		t.Fatal("repeated close ran provider teardown again")
	}
}

func TestApplicationHubRetiredTexturesSurviveProviderClose(t *testing.T) {
	first := &lifecycleHubProvider{retired: []uint64{0, 12, 12, 13}, closeRetired: []uint64{18, 19}}
	last := &lifecycleHubProvider{retired: []uint64{13, 14, 0}, closeRetired: []uint64{19, 20}}
	h := newApplicationHub(first, &hubProvider{}, last)
	if got, want := h.RetiredTextures(), []uint64{12, 13, 14}; !reflect.DeepEqual(got, want) {
		t.Fatalf("retired textures = %v, want %v", got, want)
	}
	if first.drains != 1 || last.drains != 1 {
		t.Fatal("retirement did not drain each provider once")
	}
	if got := h.RetiredTextures(); len(got) != 0 {
		t.Fatalf("retirement returned already-drained textures: %v", got)
	}
	// These textures become obsolete before shutdown but have not yet reached
	// the GPU retirement step when providers are closed.
	first.retired, last.retired = []uint64{15, 16}, []uint64{16, 17}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if got, want := h.RetiredTextures(), []uint64{15, 16, 18, 19, 17, 20}; !reflect.DeepEqual(got, want) {
		t.Fatalf("shutdown lost pending or teardown texture retirements: got %v, want %v", got, want)
	}
	if got := h.RetiredTextures(); len(got) != 0 {
		t.Fatalf("shutdown retirement repeated textures: %v", got)
	}
}

func TestApplicationHubStopsRoutingBeforeProviderTeardown(t *testing.T) {
	p := &lifecycleHubProvider{hubProvider: hubProvider{surfaces: []experience.ApplicationSurface{{ID: 7}}}}
	h := newApplicationHub(p)
	id := h.Surfaces()[0].ID
	h.Focus(id)
	readsBeforeClose := p.surfaceReads
	checkClosed := func() {
		t.Helper()
		h.Focus(id)
		h.Send(id, experience.Event{})
		h.Resize(id, 800, 600)
		h.CloseApplication(id)
		h.seat(experience.Event{})
		if _, ok := h.ApplicationCursor(id); ok {
			t.Error("closed hub returned a provider cursor")
		}
		if surfaces := h.Surfaces(); len(surfaces) != 0 {
			t.Errorf("closed hub retained surfaces: %v", surfaces)
		}
		if key, err := h.LaunchApplication("terminal"); key != "" || !errors.Is(err, errApplicationHubClosed) {
			t.Errorf("closed hub accepted a launch: key=%q err=%v", key, err)
		}
		if err := h.Poll(); !errors.Is(err, errApplicationHubClosed) {
			t.Errorf("closed hub accepted a poll: %v", err)
		}
		if h.focused != 0 || p.focusCalls != 1 || p.sent != 0 || p.resized != 0 || p.closed != 0 || p.seated != 0 || p.launches != 0 || p.cursors != 0 || p.polls != 0 || p.surfaceReads != readsBeforeClose {
			t.Error("closed hub dispatched work to a provider")
		}
	}
	p.onClose = checkClosed
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	checkClosed()
}

func TestApplicationHubEmptyLifecycle(t *testing.T) {
	h := newApplicationHub()
	if err := h.Poll(); err != nil {
		t.Fatal(err)
	}
	if retired := h.RetiredTextures(); len(retired) != 0 {
		t.Fatalf("empty hub returned retirements: %v", retired)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
}
