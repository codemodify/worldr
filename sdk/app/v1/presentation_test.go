package app

import (
	"errors"
	"fmt"
	"testing"

	"github.com/codemodify/worldr/internal/platform/linux/native"
)

type interruptedPresentation struct {
	resizeErrors, presentErrors []error
	resizes                     [][2]uint32
	clears                      [][4]float32
}

func (p *interruptedPresentation) Resize(width, height uint32) error {
	p.resizes = append(p.resizes, [2]uint32{width, height})
	if len(p.resizeErrors) == 0 {
		return nil
	}
	err := p.resizeErrors[0]
	p.resizeErrors = p.resizeErrors[1:]
	return err
}
func (p *interruptedPresentation) RenderFrame(_ Frame, clear [4]float32, _ []byte) error {
	p.clears = append(p.clears, clear)
	if len(p.presentErrors) == 0 {
		return nil
	}
	err := p.presentErrors[0]
	p.presentErrors = p.presentErrors[1:]
	return err
}

func TestWindowPresentationSurvivesResizeMinimizeAndRestore(t *testing.T) {
	gpu := &interruptedPresentation{
		resizeErrors:  []error{fmt.Errorf("surface changed during resize: %w", native.ErrOutOfDate), native.ErrNotReady, nil},
		presentErrors: []error{native.ErrOutOfDate, native.ErrNotReady, nil},
	}
	state := presentationState{}
	// The compositor may invalidate a swapchain without changing its extent.
	if presented, err := state.present(gpu, Frame{}, [4]float32{}); err != nil || presented || !state.resizePending || !state.redrawPending {
		t.Fatalf("out-of-date presentation was fatal or lost its redraw: %+v, %v", state, err)
	}
	// Minimize can temporarily report zero extent; no invalid resize is issued.
	if ready, err := state.resize(gpu, 0, 0); err != nil || ready || len(gpu.resizes) != 0 {
		t.Fatalf("zero extent resize: ready=%v err=%v calls=%v", ready, err, gpu.resizes)
	}
	// Resizing can race another configure or remain unavailable until restored.
	for i := 0; i < 2; i++ {
		if ready, err := state.resize(gpu, 1200, 800); err != nil || ready || !state.resizePending || !state.redrawPending {
			t.Fatalf("transient resize %d lost pending work: %+v, %v", i, state, err)
		}
	}
	if ready, err := state.resize(gpu, 1200, 800); err != nil || !ready || state.resizePending || !state.redrawPending {
		t.Fatalf("restore resize: ready=%v state=%+v err=%v", ready, state, err)
	}
	// An acquired-image timeout needs a future frame, not repeated recreation.
	if presented, err := state.present(gpu, Frame{}, [4]float32{}); err != nil || presented || state.resizePending || !state.redrawPending {
		t.Fatalf("not-ready presentation: state=%+v err=%v", state, err)
	}
	if presented, err := state.present(gpu, Frame{}, [4]float32{}); err != nil || !presented || state.resizePending || state.redrawPending {
		t.Fatalf("restored presentation: state=%+v err=%v", state, err)
	}
	if len(gpu.resizes) != 3 || len(gpu.clears) != 3 {
		t.Fatalf("unexpected recovery operations: resizes=%d presents=%d", len(gpu.resizes), len(gpu.clears))
	}
	for _, clear := range gpu.clears {
		if clear[3] != 0 {
			t.Fatal("retry changed transparent clear")
		}
	}
}

func TestPresentationKeepsPermanentFailuresActionable(t *testing.T) {
	for _, failure := range []error{native.ErrSurfaceLost, native.ErrDeviceLost, native.ErrOutOfMemory, errors.New("invalid frame")} {
		state := presentationState{}
		gpu := &interruptedPresentation{resizeErrors: []error{failure}, presentErrors: []error{failure}}
		if _, err := state.resize(gpu, 640, 480); !errors.Is(err, failure) {
			t.Fatalf("resize swallowed permanent failure %v: %v", failure, err)
		}
		if _, err := state.present(gpu, Frame{}, [4]float32{}); !errors.Is(err, failure) {
			t.Fatalf("presentation swallowed permanent failure %v: %v", failure, err)
		}
	}
}
