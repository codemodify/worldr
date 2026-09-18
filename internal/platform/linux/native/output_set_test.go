package native

import (
	"errors"
	"image"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

type outputFixture struct {
	width, height                          uint32
	stats                                  MemoryStats
	desired                                uint64
	renderCount, closeCount, recoverCount  int
	textureIDs, geometryIDs                []uint64
	frame                                  render.Frame
	budgetError, renderError, recoverError error
}

func (f *outputFixture) Size() (uint32, uint32) { return f.width, f.height }
func (f *outputFixture) RenderFrame(frame render.Frame, _ [4]float32, _ []byte) error {
	if f.renderError != nil {
		return f.renderError
	}
	if f.desired > f.stats.BudgetBytes {
		return ErrOutOfMemory
	}
	f.stats.AllocatedBytes = max(f.stats.AllocatedBytes, f.desired)
	f.stats.PeakBytes = max(f.stats.PeakBytes, f.stats.AllocatedBytes)
	f.renderCount++
	f.frame = frame
	return nil
}
func (f *outputFixture) SetSceneAtlas(render.Atlas) error { return nil }
func (f *outputFixture) ReleaseTexture(id uint64) error {
	f.textureIDs = append(f.textureIDs, id)
	return nil
}
func (f *outputFixture) ReleaseGeometry(id uint64) error {
	f.geometryIDs = append(f.geometryIDs, id)
	return nil
}
func (f *outputFixture) SetMemoryBudget(b uint64) error {
	if f.budgetError != nil {
		return f.budgetError
	}
	if b < f.stats.AllocatedBytes {
		return ErrOutOfMemory
	}
	f.stats.BudgetBytes = b
	return nil
}
func (f *outputFixture) MemoryStats() MemoryStats { return f.stats }
func (f *outputFixture) Recover() error           { f.recoverCount++; return f.recoverError }
func (f *outputFixture) Close()                   { f.closeCount++ }
func outputFixtures() ([]outputState, *outputFixture, *outputFixture) {
	a := &outputFixture{width: 64, height: 64, stats: MemoryStats{AllocatedBytes: 100, PeakBytes: 100, BudgetBytes: 1000, Images: 1, Buffers: 2}}
	b := &outputFixture{width: 64, height: 48, stats: a.stats}
	return []outputState{{OutputInfo: OutputInfo{ID: 17, Bounds: image.Rect(0, 0, 64, 64)}, target: a}, {OutputInfo: OutputInfo{ID: 42, Bounds: image.Rect(64, 16, 128, 64)}, target: b}}, a, b
}
func TestOutputSetCropsExtendedDesktopAndMapsInputWithoutMutatingFrame(t *testing.T) {
	targets, a, b := outputFixtures()
	set, err := newOutputSet(targets, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if w, h := set.Size(); w != 128 || h != 64 {
		t.Fatal("incorrect desktop extent", w, h)
	}
	texture, err := render.NewTexture(1, 1, []byte{100, 50, 25, 255})
	if err != nil {
		t.Fatal(err)
	}
	projection := [16]float32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	output := render.OutputTransform{Exposure: .2, BloomStrength: .4, BloomThreshold: .7, BloomRadius: 5}
	frame := render.Frame{LinearColor: true, Output: output, Vertices: []render.Vertex{{X: 72, Y: 24, R: .5, A: 1}}, Commands: []render.Command{
		{Kind: render.OverlayCommand, First: 0, Count: 0},
		{Kind: render.SceneCommand, View: render.View{Projection: projection, Viewport: [4]float32{10, 12, 110, 40}, TransparencyLayers: 9}, Draws: []render.Draw{{Texture: texture, UV: [4]float32{.2, .1, .7, .8}}}},
		{Kind: render.ImageCommand, Image: render.Image{Texture: texture, Bounds: [4]float32{50, 20, 40, 30}}},
	}}
	originalVertices := append([]render.Vertex(nil), frame.Vertices...)
	originalCommands := append([]render.Command(nil), frame.Commands...)
	if err := set.RenderFrame(frame, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(frame.Vertices, originalVertices) || !reflect.DeepEqual(frame.Commands, originalCommands) {
		t.Fatal("output crop modified caller frame")
	}
	if a.frame.Vertices[0] != frame.Vertices[0] || b.frame.Vertices[0].X != 8 || b.frame.Vertices[0].Y != 8 || !b.frame.LinearColor || b.frame.Output != output {
		t.Fatal("overlay translation changed color/coordinates")
	}
	if b.frame.Commands[1].View.Projection != projection || b.frame.Commands[1].View.Viewport != ([4]float32{-54, -4, 110, 40}) || b.frame.Commands[1].Draws[0].Texture != texture || b.frame.Commands[1].View.TransparencyLayers != 9 {
		t.Fatal("output crop changed projection or retained content")
	}
	if b.frame.Commands[2].Image.Bounds != ([4]float32{-14, 4, 40, 30}) {
		t.Fatal("image overlay did not cross output seam")
	}
	if x, y, ok := set.LocalToGlobal(42, 8, 8); !ok || x != 72 || y != 24 {
		t.Fatal("local input mapping", x, y, ok)
	}
	if id, x, y, ok := set.OutputAt(64, 20); !ok || id != 42 || x != 0 || y != 4 {
		t.Fatal("shared edge input mapping", id, x, y, ok)
	}
	for _, point := range [][2]float32{{72, 8}, {128, 20}, {-1, 2}, {float32(math.NaN()), 2}, {2, float32(math.Inf(1))}} {
		if _, _, _, ok := set.OutputAt(point[0], point[1]); ok {
			t.Fatal("gap/outside/nonfinite input assigned", point)
		}
	}
	if _, _, ok := set.LocalToGlobal(42, 64, 1); ok {
		t.Fatal("far output edge accepted")
	}
	metadata := set.Outputs()
	metadata[0].Bounds = image.Rectangle{}
	if set.Outputs()[0].Bounds.Empty() {
		t.Fatal("metadata exposes mutable topology")
	}
}
func TestOutputSetAggregateBudgetAndAllDeviceRetirementRecovery(t *testing.T) {
	targets, a, b := outputFixtures()
	set, err := newOutputSet(targets, 1000)
	if err != nil {
		t.Fatal(err)
	}
	a.desired, b.desired = 600, 600
	if err := set.RenderFrame(render.Frame{}, [4]float32{}); !errors.Is(err, ErrOutOfMemory) || !strings.Contains(err.Error(), "42") {
		t.Fatal("second device exceeded aggregate budget", err)
	}
	if a.renderCount != 1 || b.renderCount != 0 || set.MemoryStats().AllocatedBytes != 700 || a.stats.BudgetBytes != 900 || b.stats.BudgetBytes != 400 {
		t.Fatal("aggregate reservation is not bounded", a.stats, b.stats, set.MemoryStats())
	}
	if err := set.SetMemoryBudget(699); !errors.Is(err, ErrOutOfMemory) || set.MemoryStats().BudgetBytes != 1000 {
		t.Fatal("rejected aggregate budget altered live limit", err)
	}
	if err := set.SetMemoryBudget(1600); err != nil {
		t.Fatal(err)
	}
	if err := set.RenderFrame(render.Frame{}, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if stats := set.MemoryStats(); stats.AllocatedBytes != 1200 || stats.Images != 2 || stats.Buffers != 4 || stats.PeakBytes < 1200 {
		t.Fatal("incorrect aggregate accounting", stats)
	}
	a.recoverError = ErrDeviceLost
	if err := set.Recover(); !errors.Is(err, ErrDeviceLost) || a.recoverCount != 1 || b.recoverCount != 1 {
		t.Fatal("recovery stopped before visiting all outputs", err)
	}
	if err := set.ReleaseTexture(9); err != nil {
		t.Fatal(err)
	}
	if err := set.ReleaseGeometry(4); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.textureIDs, []uint64{9}) || !reflect.DeepEqual(b.textureIDs, a.textureIDs) || !reflect.DeepEqual(a.geometryIDs, []uint64{4}) || !reflect.DeepEqual(b.geometryIDs, a.geometryIDs) {
		t.Fatal("shared resources not retired on every device")
	}
	set.Close()
	set.Close()
	if a.closeCount != 1 || b.closeCount != 1 || set.MemoryStats() != (MemoryStats{}) || set.Primary() != nil {
		t.Fatal("idempotent collection ownership failed")
	}
	if err := set.RenderFrame(render.Frame{}, [4]float32{}); err == nil {
		t.Fatal("closed collection rendered")
	}
}

func TestOutputSetFailureRecoveryKeepsHealthyOutputResident(t *testing.T) {
	targets, a, b := outputFixtures()
	set, err := newOutputSet(targets, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	b.renderError = ErrDeviceLost
	err = set.RenderFrame(render.Frame{}, [4]float32{})
	if !errors.Is(err, ErrDeviceLost) {
		t.Fatal("device loss was not preserved", err)
	}
	var output *OutputError
	if !errors.As(err, &output) || output.OutputID != 42 {
		t.Fatalf("failed connector identity was lost: %#v", err)
	}
	if err := set.RecoverAffected(err); err != nil {
		t.Fatal(err)
	}
	if a.recoverCount != 0 || b.recoverCount != 1 {
		t.Fatalf("healthy output was rebuilt: left=%d right=%d", a.recoverCount, b.recoverCount)
	}

	b.renderError = nil
	if err := set.RenderFrame(render.Frame{}, [4]float32{}); err != nil {
		t.Fatal("render after isolated recovery", err)
	}
	if a.renderCount != 2 || b.renderCount != 1 {
		t.Fatalf("outputs did not resume together: left=%d right=%d", a.renderCount, b.renderCount)
	}

	mixed := errors.Join(
		&OutputError{OutputID: 42, Err: ErrOutOfMemory},
		&OutputError{OutputID: 17, Err: ErrDeviceLost},
	)
	if err := set.RecoverAffected(mixed); err != nil {
		t.Fatal("scoped device loss was obscured by an allocation error", err)
	}
	if a.recoverCount != 1 || b.recoverCount != 1 {
		t.Fatalf("non-device failure rebuilt an output: left=%d right=%d", a.recoverCount, b.recoverCount)
	}

	if err := set.RecoverAffected(ErrDeviceLost); err != nil {
		t.Fatal("unscoped device loss did not retain conservative recovery", err)
	}
	if a.recoverCount != 2 || b.recoverCount != 2 {
		t.Fatalf("unscoped failure did not rebuild all outputs: left=%d right=%d", a.recoverCount, b.recoverCount)
	}
	foreign := &OutputError{OutputID: 99, Err: ErrDeviceLost}
	if err := set.RecoverAffected(foreign); err == nil {
		t.Fatal("stale connector failure was accepted")
	}
}
func TestOutputSetRejectsBadTopologyBeforeTakingOwnership(t *testing.T) {
	cases := map[string]func([]outputState) []outputState{
		"empty":        func(_ []outputState) []outputState { return nil },
		"duplicate id": func(v []outputState) []outputState { v[1].ID = v[0].ID; return v },
		"overlap":      func(v []outputState) []outputState { v[1].Bounds = image.Rect(32, 16, 96, 64); return v },
		"negative":     func(v []outputState) []outputState { v[1].Bounds = image.Rect(-64, 16, 0, 64); return v },
		"oversized":    func(v []outputState) []outputState { v[1].Bounds = image.Rect(7680, 16, 7744, 64); return v },
		"wrong extent": func(v []outputState) []outputState { v[1].Bounds.Max.X--; return v },
	}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			targets, a, b := outputFixtures()
			if _, err := newOutputSet(alter(targets), 1000); err == nil {
				t.Fatal("invalid topology accepted")
			}
			if a.closeCount != 0 || b.closeCount != 0 || a.stats.BudgetBytes != 1000 || b.stats.BudgetBytes != 1000 {
				t.Fatal("rejected constructor took ownership or changed budgets")
			}
		})
	}
	targets, a, b := outputFixtures()
	b.budgetError = errors.New("rejected")
	if _, err := newOutputSet(targets, 700); err == nil {
		t.Fatal("failed budget accepted")
	}
	if a.stats.BudgetBytes != 1000 || a.closeCount != 0 || b.closeCount != 0 {
		t.Fatal("failed budget did not restore caller ownership/limits")
	}
}
