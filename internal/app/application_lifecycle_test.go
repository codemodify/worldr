package app

import (
	"errors"
	"io"
	"testing"

	"github.com/codemodify/worldr/internal/platform/linux/apps"
)

type lifecycleApplicationServer struct {
	adapterServer
	closeErr                error
	requests, closes, polls int
}

func (s *lifecycleApplicationServer) RequestClose() error {
	s.requests++
	s.current = nil
	return nil
}

func (s *lifecycleApplicationServer) Close() error {
	s.closes++
	return s.closeErr
}

func (s *lifecycleApplicationServer) Poll() ([]apps.Surface, error) {
	s.polls++
	return s.current, nil
}

func TestCompatibilityProviderLifecycleRetiresLiveImagesAndReportsCloseFailure(t *testing.T) {
	closeErr := errors.New("display shutdown failed")
	server := &lifecycleApplicationServer{closeErr: closeErr}
	server.current = []apps.Surface{{ID: 7, Width: 2, Height: 2, Revision: 1, Pixels: make([]byte, 16)}}
	provider := &applicationController{server: server, images: make(map[uint64]*applicationImage), output: io.Discard}
	hub := newApplicationHub(provider)
	if err := hub.Poll(); err != nil {
		t.Fatal(err)
	}
	texture := hub.Surfaces()[0].Texture.ID()
	if err := hub.Close(); !errors.Is(err, closeErr) {
		t.Fatal("compatibility close failure was lost", err)
	}
	if len(provider.Surfaces()) != 0 || len(provider.images) != 0 {
		t.Fatal("closed compatibility provider kept live images")
	}
	if retired := hub.RetiredTextures(); len(retired) != 1 || retired[0] != texture {
		t.Fatal("live texture was not retired on provider shutdown", retired)
	}
	polls := server.polls
	if err := provider.Poll(); !errors.Is(err, apps.ErrClosed) || server.polls != polls {
		t.Fatal("closed provider polled its destroyed protocol server", err)
	}
	if err := hub.Close(); !errors.Is(err, closeErr) {
		t.Fatal("repeated shutdown lost original error", err)
	}
	if err := provider.Close(); !errors.Is(err, closeErr) || server.closes != 1 || server.requests != 1 {
		t.Fatal("compatibility shutdown repeated or lost original error", err)
	}
	if retired := hub.RetiredTextures(); len(retired) != 0 {
		t.Fatal("shutdown retired the same texture twice", retired)
	}
}
