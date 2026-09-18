package sdkhost

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/scene"
	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
)

func testTexture(id nativeapp.ResourceID, revision uint64, width, height int, value byte) nativeapp.TextureUpdate {
	return nativeapp.TextureUpdate{
		ID: id, Revision: revision, Width: width, Height: height,
		Rect:   nativeapp.Rect{Width: width, Height: height},
		Pixels: bytes.Repeat([]byte{value, value, value, 255}, width*height),
	}
}

func bareProvider() *Provider {
	return &Provider{
		manifest: nativeapp.Manifest{ID: "dev.worldr.host-test", Name: "Host test"},
		textures: make(map[nativeapp.ResourceID]textureResource), meshes: make(map[nativeapp.ResourceID]*scene.Mesh),
		semantics: make(map[uint64]nativeui.SemanticTree), textInput: make(map[uint64]experience.TextInputState), surfaceKeys: make(map[uint64]string),
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func TestApplyConvertsRetainedResourcesSurfacesAndRetirement(t *testing.T) {
	p := bareProvider()
	initial := nativeapp.Snapshot{
		Textures: []nativeapp.TextureUpdate{testTexture(1, 1, 8, 6, 30)},
		Meshes: []nativeapp.MeshResource{{
			ID: 2, Vertices: []nativeapp.Vec3{{X: 0}, {X: 1}, {Y: 1}}, Indices: []uint32{0, 1, 2},
		}},
		Surfaces: []nativeapp.Surface{{
			ID: 3, Key: "main", Title: "External instrument", Texture: 1, FrameStyle: nativeapp.FrameCinematic,
			Spatial: &nativeapp.SpatialContent{Objects: []nativeapp.SpatialObject{{ID: 7, Mesh: 2, Translucent: true, Material: nativeapp.Material{Transmission: .72, Refraction: .64, RefractionBlur: .18}}}},
			Semantics: nativeapp.SemanticTree{Nodes: []nativeapp.SemanticNode{
				{ID: "toggle", Role: nativeapp.RoleButton, Label: "Toggle", Bounds: nativeapp.Rect{Width: 3, Height: 2}},
				{ID: "gain", Role: nativeapp.RoleSlider, Label: "Gain", Bounds: nativeapp.Rect{Y: 2, Width: 3, Height: 1}},
				{ID: "plot", Role: nativeapp.RoleImage, Label: "Plot", Bounds: nativeapp.Rect{Y: 3, Width: 3, Height: 1}},
				{ID: "document", Role: nativeapp.RoleDocument, Label: "Report", Bounds: nativeapp.Rect{Y: 4, Width: 3, Height: 1}},
				{ID: "status", Role: nativeapp.RoleStatus, Label: "Ready", Bounds: nativeapp.Rect{Y: 5, Width: 3, Height: 1}},
			}, FocusedID: "toggle"},
			TextInput: nativeapp.TextInputState{Enabled: true, ContextID: "query-1", Surrounding: "ion", Cursor: 3, Anchor: 3, CursorRect: nativeapp.Rect{X: 2, Y: 2, Width: 1, Height: 2}},
		}},
	}
	if err := p.apply(initial); err != nil {
		t.Fatal(err)
	}
	if len(p.surfaces) != 1 || p.surfaces[0].Key != "sdk:dev.worldr.host-test/main" || p.surfaces[0].Spatial == nil || p.surfaces[0].Spatial.Objects[0].Node.Mesh == nil {
		t.Fatalf("surface conversion: %+v", p.surfaces)
	}
	if got := p.surfaces[0].Spatial.Objects[0].Node.Material; got.Transmission != .72 || got.Refraction != .64 || got.RefractionBlur != .18 {
		t.Fatal("surface conversion lost thin-glass optics", got)
	}
	if tree := p.Semantics(3); len(tree.Nodes) != 5 || tree.FocusedID != "toggle" || tree.Nodes[0].Label != "Toggle" || tree.Nodes[1].Role != nativeui.RoleSlider || tree.Nodes[2].Role != nativeui.RoleImage || tree.Nodes[3].Role != nativeui.RoleDocument || tree.Nodes[4].Role != nativeui.RoleStatus {
		t.Fatalf("semantic conversion: %+v", tree)
	}
	p.focused = 3
	if state := p.TextInput(3); !state.Enabled || state.ContextID != "query-1" || state.Cursor != 3 {
		t.Fatalf("text input conversion: %+v", state)
	}
	before := p.textures[1].texture.Revision()
	partial := nativeapp.Snapshot{
		Textures: []nativeapp.TextureUpdate{{ID: 1, Revision: 2, Width: 8, Height: 6, Rect: nativeapp.Rect{X: 2, Y: 1, Width: 2, Height: 1}, Pixels: bytes.Repeat([]byte{90}, 8)}},
		Surfaces: initial.Surfaces,
	}
	if err := p.apply(partial); err != nil {
		t.Fatal(err)
	}
	if p.textures[1].texture.Revision() != before+1 {
		t.Fatal("partial texture update did not reach retained renderer resource")
	}
	textureID := p.textures[1].texture.ID()
	geometryID := p.meshes[2].Geometry().ID()
	if err := p.apply(nativeapp.Snapshot{RetireTextures: []nativeapp.ResourceID{1}, RetireMeshes: []nativeapp.ResourceID{2}}); err != nil {
		t.Fatal(err)
	}
	if got := p.RetiredTextures(); len(got) != 1 || got[0] != textureID {
		t.Fatalf("texture retirement: %v", got)
	}
	if got := p.RetiredGeometryIDs(); len(got) != 1 || got[0] != geometryID {
		t.Fatalf("geometry retirement: %v", got)
	}
}

func TestRepeatedManifestInstancesReceiveStableDistinctKeyNamespaces(t *testing.T) {
	p := bareProvider()
	snapshot := nativeapp.Snapshot{
		Textures: []nativeapp.TextureUpdate{testTexture(1, 1, 2, 2, 30)},
		Surfaces: []nativeapp.Surface{{ID: 1, Key: "main", Title: "Repeated tool", Texture: 1}},
	}
	if err := p.apply(snapshot); err != nil {
		t.Fatal(err)
	}
	if got := p.Surfaces()[0].Key; got != "sdk:dev.worldr.host-test/main" {
		t.Fatal("first instance key changed", got)
	}
	if err := p.SetInstanceOrdinal(2); err != nil {
		t.Fatal(err)
	}
	if got := p.Surfaces()[0].Key; got != "sdk:dev.worldr.host-test/instance-2/main" {
		t.Fatal("repeated instance key collided", got)
	}
	// A later complete snapshot must retain the assigned namespace.
	if err := p.apply(nativeapp.Snapshot{Surfaces: snapshot.Surfaces}); err != nil {
		t.Fatal(err)
	}
	if got := p.Surfaces()[0].Key; got != "sdk:dev.worldr.host-test/instance-2/main" {
		t.Fatal("snapshot discarded instance namespace", got)
	}
	if err := p.SetInstanceOrdinal(0); err == nil {
		t.Fatal("invalid instance ordinal accepted")
	}
}

func TestComposedSDKKeysRemainBoundedAndCollisionResistant(t *testing.T) {
	namespace := strings.Repeat("a", 128)
	first := sdkSurfaceKey(namespace, strings.Repeat("b", 128))
	second := sdkSurfaceKey(namespace, strings.Repeat("b", 127)+"c")
	if len(first) != maxWorkspaceKeyBytes || len(second) != maxWorkspaceKeyBytes || first == second {
		t.Fatalf("bounded SDK keys: %d %d equal=%v", len(first), len(second), first == second)
	}
	if again := sdkSurfaceKey(namespace, strings.Repeat("b", 128)); again != first {
		t.Fatal("bounded SDK key is not deterministic")
	}
}

func TestProviderFailureWithdrawsContentWithoutFailingWorkspace(t *testing.T) {
	p := bareProvider()
	if err := p.apply(nativeapp.Snapshot{
		Textures: []nativeapp.TextureUpdate{testTexture(1, 1, 2, 2, 1)},
		Surfaces: []nativeapp.Surface{{ID: 1, Key: "main", Title: "Failing tool", Texture: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	_, p.cancel = context.WithCancel(context.Background())
	p.stdin, p.stdout = nopWriteCloser{io.Discard}, io.NopCloser(bytes.NewReader(nil))
	var log bytes.Buffer
	p.log = &log
	p.err = errors.New("protocol failure")
	if err := p.Poll(); err != nil {
		t.Fatalf("external failure escaped into workspace: %v", err)
	}
	if len(p.Surfaces()) != 0 || len(p.RetiredTextures()) != 1 || !bytes.Contains(log.Bytes(), []byte("workspace remains open")) {
		t.Fatalf("failed provider did not withdraw cleanly: surfaces=%d log=%q", len(p.Surfaces()), log.String())
	}
}

func TestSeatForwardsOnlySurfaceIndependentKeyboardState(t *testing.T) {
	p := bareProvider()
	p.requests = make(chan queuedRequest, 8)

	for _, event := range []experience.Event{
		{Kind: experience.PointerMove, X: 12, Y: 8},
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary},
		{Kind: experience.KeyInput, Keycode: 30, Pressed: true},
		{Kind: experience.TextCommit, Text: "x"},
		{Kind: experience.TextPreedit, Text: "y"},
	} {
		p.Seat(event)
	}
	if got := len(p.requests); got != 0 {
		t.Fatalf("seat forwarded %d surface-bound input events with surface zero", got)
	}

	allowed := []experience.Event{
		{Kind: experience.KeymapChanged, Keymap: "xkb_keymap {}"},
		{Kind: experience.KeyboardModifiers, Depressed: 1, Group: 2},
		{Kind: experience.KeyboardRepeatInfo, RepeatRate: 25, RepeatDelay: 400},
		{Kind: experience.KeyboardCancel},
	}
	for _, event := range allowed {
		p.Seat(event)
	}
	for index, want := range allowed {
		select {
		case queued := <-p.requests:
			request := queued.request
			if request.Kind != nativeapp.RequestInput || request.Surface != 0 || request.Event == nil || request.Event.Kind != eventKindToSDK(want.Kind) {
				t.Fatalf("seat request %d: %+v", index, request)
			}
		default:
			t.Fatalf("seat omitted keyboard-state request %d", index)
		}
	}
}

type helperApplication struct {
	pending  []nativeapp.TextureUpdate
	revision uint64
	closed   bool
}

func (a *helperApplication) Manifest() nativeapp.Manifest {
	return nativeapp.Manifest{ID: "dev.worldr.sdk-helper", Name: "SDK helper"}
}
func (a *helperApplication) Start(nativeapp.Host) error {
	a.revision = 1
	a.pending = []nativeapp.TextureUpdate{testTexture(1, 1, 12, 8, 10)}
	return nil
}
func (a *helperApplication) Handle(_ nativeapp.SurfaceID, event nativeapp.Event) error {
	if event.Kind == nativeapp.PointerDown {
		a.revision++
		a.pending = []nativeapp.TextureUpdate{testTexture(1, a.revision, 12, 8, 80)}
	}
	return nil
}
func (a *helperApplication) Snapshot() nativeapp.Snapshot {
	updates := a.pending
	a.pending = nil
	if a.closed {
		return nativeapp.Snapshot{Textures: updates}
	}
	return nativeapp.Snapshot{Textures: updates, Surfaces: []nativeapp.Surface{{ID: 1, Key: "main", Title: "SDK helper", Texture: 1}}}
}
func (a *helperApplication) CloseSurface(nativeapp.SurfaceID) error { a.closed = true; return nil }
func (a *helperApplication) Close() error                           { return nil }

func TestSDKHostHelper(t *testing.T) {
	if os.Getenv("WORLDR_SDK_TEST_HELPER") != "1" {
		return
	}
	if err := nativeapp.Serve(context.Background(), &helperApplication{}, os.Stdin, os.Stdout); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestProviderNegotiatesExternalProcessWithoutBlockingPoll(t *testing.T) {
	t.Setenv("WORLDR_SDK_TEST_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p, err := Open(context.Background(), executable, []string{"-test.run=^TestSDKHostHelper$"}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if len(p.Surfaces()) != 1 || p.Surfaces()[0].Key != "sdk:dev.worldr.sdk-helper/main" {
		t.Fatalf("handshake surface: %+v", p.Surfaces())
	}
	start := time.Now()
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("Poll waited for process I/O")
	}
	p.Focus(1)
	p.Send(1, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 4, Y: 3})
	deadline := time.Now().Add(2 * time.Second)
	for p.textures[1].revision != 2 && time.Now().Before(deadline) {
		if err := p.Poll(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if p.textures[1].revision != 2 {
		t.Fatal("external input did not produce a retained update")
	}
	p.CloseApplication(1)
	for len(p.Surfaces()) != 0 && time.Now().Before(deadline) {
		if err := p.Poll(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if len(p.Surfaces()) != 0 {
		t.Fatal("external app did not close its surface")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if len(p.RetiredTextures()) != 1 {
		t.Fatal("provider close did not retire retained texture")
	}
}
