package modelapp

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

const triangleOBJ = "v 0 0 0\nv 3 0 0\nv 0 4 0\no plate\nf 1 2 3\n"

func TestModelReadersRetainComponentsAndRejectInvalidGeometry(t *testing.T) {
	m, err := parseOBJ(context.Background(), []byte(triangleOBJ+"g back\nf -1 -2 -3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Components) != 2 || m.Components[0].Name != "plate" || m.Components[1].Name != "back" || m.Triangles != 2 || m.Maximum != (scene.Vec3{X: 3, Y: 4}) {
		t.Fatalf("wrong OBJ geometry: %+v", m)
	}
	for _, data := range []string{"v NaN 0 0\nf 1 1 1", "v 0 0 0\nf 1 2 3", triangleOBJ + "f 1 2 3 1", "v 0 0 0\nf 1 1 1"} {
		if _, err := parseOBJ(context.Background(), []byte(data)); err == nil {
			t.Fatal("accepted invalid OBJ", data)
		}
	}
	stl := []byte("solid plate\nfacet normal 0 0 1\nouter loop\nvertex 0 0 0\nvertex 3 0 0\nvertex 0 4 0\nendloop\nendfacet\nendsolid\n")
	if m, err := parseSTL(context.Background(), stl); err != nil || m.Triangles != 1 {
		t.Fatal("ASCII STL", m, err)
	}
	data := make([]byte, 134)
	copy(data, "solid binary name")
	binary.LittleEndian.PutUint32(data[80:], 1)
	points := []float32{0, 0, 0, 3, 0, 0, 0, 4, 0}
	for i, v := range points {
		binary.LittleEndian.PutUint32(data[96+i*4:], math.Float32bits(v))
	}
	if m, err := parseSTL(context.Background(), data); err != nil || m.Triangles != 1 {
		t.Fatal("binary STL", m, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := parseSTL(ctx, data); err == nil {
		t.Fatal("canceled decoder continued")
	}
}
func modelFixture(t *testing.T) (*Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plate.obj")
	if err := os.WriteFile(path, []byte(triangleOBJ), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	t.Cleanup(func() { m.Close() })
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.OpenFile(file, "plate.obj"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	waitModel(t, m)
	return m, path
}
func waitModel(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := m.Poll(); err != nil {
			t.Fatal(err)
		}
		loading := false
		for _, v := range m.slots {
			loading = loading || v != nil && (v.loading || v.saving)
		}
		if !loading {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("model worker did not finish")
}
func TestModelMeasurementsAnnotationsSaveAndRestore(t *testing.T) {
	m, path := modelFixture(t)
	v := m.slots[0]
	if v.model == nil {
		t.Fatal(v.message)
	}
	m.Focus(v.id)
	v.action("measure")
	click := func(p scene.Vec3) {
		local := v.transform().TransformPoint(p)
		m.Send(v.id, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 480, Y: 300, SpatialObject: 2, SpatialPoint: [3]float32{local.X, local.Y, local.Z}})
		m.Send(v.id, experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: 480, Y: 300})
	}
	click(scene.Vec3{X: 3})
	click(scene.Vec3{Y: 4})
	if len(v.state.View.Measurements) != 1 || math.Abs(float64(v.state.View.Measurements[0].Distance-5)) > .00001 {
		t.Fatal("3-4-5 measurement not preserved", v.state.View.Measurements)
	}
	v.action("annotate")
	click(scene.Vec3{X: 1, Y: 1})
	if !v.editing {
		t.Fatal("annotation did not enter text input")
	}
	for _, key := range []uint32{49, 24, 20, 18, 28} {
		m.Send(v.id, experience.Event{Kind: experience.KeyInput, Keycode: key, Pressed: true})
	}
	if len(v.state.View.Annotations) != 1 || v.state.View.Annotations[0].Text != "note" {
		t.Fatal("annotation text routing failed", v.state.View.Annotations)
	}
	v.action("save")
	waitModel(t, m)
	if !strings.HasPrefix(v.message, "Saved ") {
		t.Fatal(v.message)
	}
	want := cloneView(v.state.View)
	tool := path + ".worldr-model.json"
	m.CloseApplication(v.id)
	if len(m.Surfaces()) != 0 || len(m.RetiredGeometryIDs()) != 1 || len(m.RetiredTextures()) != 1 {
		t.Fatal("closed inspector leaked its resources")
	}
	file, err := os.Open(tool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.OpenFile(file, filepath.Base(tool)); err != nil {
		file.Close()
		t.Fatal(err)
	}
	waitModel(t, m)
	if got := m.slots[0].state.View; !reflect.DeepEqual(got, want) {
		t.Fatalf("tool document changed after reopen: %+v != %+v", got, want)
	}
	saved := m.SessionStates()[0]
	m.CloseApplication(m.slots[0].id)
	saved.Key = "native:model-3"
	if _, err = m.Restore(saved); err != nil {
		t.Fatal(err)
	}
	waitModel(t, m)
	if m.slots[2] == nil || !reflect.DeepEqual(m.slots[2].state.View, want) || m.slots[2].focused {
		t.Fatal("session failed to restore exact slot/document or stole focus")
	}
}
func TestModelSaveValidationCannotOverwriteSource(t *testing.T) {
	m, path := modelFixture(t)
	s := m.SessionStates()[0]
	s.Document = path
	if err := writeDocument(s); err == nil {
		t.Fatal("notes overwrote source model")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != triangleOBJ {
		t.Fatal("source model changed", err)
	}
}
func TestMultipleNativeModelsKeepIndependentGeometryAndFocus(t *testing.T) {
	m, path := modelFixture(t)
	first := m.slots[0]
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.OpenFile(file, "plate.obj"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	waitModel(t, m)
	second := m.slots[1]
	m.Focus(first.id)
	m.Send(second.id, experience.Event{Kind: experience.PointerScroll, ScrollY: -10})
	m.Poll()
	if len(m.Surfaces()) != 2 || first.state.View.Zoom != 1 || second.state.View.Zoom <= 1 || !first.focused || second.focused {
		t.Fatal("model state/focus crossed applications")
	}
	m.CloseApplication(first.id)
	if len(m.Surfaces()) != 1 || m.Surfaces()[0].ID != second.id {
		t.Fatal("close removed sibling")
	}
}

func TestClosingModelDoesNotWaitForWorkers(t *testing.T) {
	m, _ := modelFixture(t)
	v := m.slots[0]
	v.loading, v.saving = true, true
	v.result, v.saveResult = make(chan loadResult, 1), make(chan error, 1)
	// Neither worker can finish until after CloseApplication returns.
	done := make(chan struct{})
	go func() { m.CloseApplication(v.id); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		v.result <- loadResult{}
		v.saveResult <- nil
		<-done
		t.Fatal("closing a window waited for a background worker")
	}
	if len(m.Surfaces()) != 0 || len(m.retiring) != 1 {
		t.Fatal("closed view remained live or lost its workers")
	}
	v.result <- loadResult{}
	v.saveResult <- nil
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	if len(m.retiring) != 0 {
		t.Fatal("completed closed workers retained")
	}
}

func TestPendingToolDocumentSurvivesSessionCheckpoint(t *testing.T) {
	m, path := modelFixture(t)
	state := m.SessionStates()[0]
	state.View.Annotations = []Annotation{{Point: scene.Vec3{X: 1}, Text: "retained note"}}
	if err := writeDocument(state); err != nil {
		t.Fatal(err)
	}
	m.CloseApplication(m.slots[0].id)
	// Hold the single parser before opening: checkpoint must not wait for parsing.
	m.loader <- struct{}{}
	file, err := os.Open(state.Document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.OpenFile(file, filepath.Base(state.Document)); err != nil {
		file.Close()
		t.Fatal(err)
	}
	saved := m.SessionStates()
	if len(saved) != 1 || !saved[0].DocumentOnly || saved[0].ResourcePath() != path+".worldr-model.json" {
		<-m.loader
		t.Fatal("pending tool document omitted from checkpoint", saved)
	}
	restored := NewManager()
	defer restored.Close()
	if _, err := restored.Restore(saved[0]); err != nil {
		<-m.loader
		t.Fatal(err)
	}
	<-m.loader
	waitModel(t, restored)
	got := restored.SessionStates()
	if len(got) != 1 || got[0].DocumentOnly || got[0].Source != path || !reflect.DeepEqual(got[0].View.Annotations, state.View.Annotations) {
		t.Fatal("pending restore lost source or notes", got)
	}
}

func TestMeshPickWinsOverOccludedToolbar(t *testing.T) {
	m, _ := modelFixture(t)
	v := m.slots[0]
	v.action("measure")
	point := v.transform().TransformPoint(scene.Vec3{X: 1})
	event := experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 50, Y: 60, SpatialObject: 2, SpatialPoint: [3]float32{point.X, point.Y, point.Z}}
	m.Send(v.id, event)
	event.Kind = experience.PointerUp
	m.Send(v.id, event)
	if v.mode != "measure" || v.pending == nil {
		t.Fatal("mesh pick activated the covered Inspect control")
	}
}

func TestUnusedOBJVerticesDoNotChangeModelFit(t *testing.T) {
	m, err := parseOBJ(context.Background(), []byte(triangleOBJ+"v 1000000 1000000 1000000\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Maximum != (scene.Vec3{X: 3, Y: 4}) {
		t.Fatal("unused vertex expanded model bounds", m.Maximum)
	}
}

func TestModelAnnotationClipboardRejectsClosedAndRefocusedTargets(t *testing.T) {
	m, _ := modelFixture(t)
	v := m.slots[0]
	m.Focus(v.id)
	v.editing = true
	v.textEpoch++
	v.note.Set("測定")
	v.note.SelectAll()
	key := experience.Event{Kind: experience.KeyInput, Keycode: 46, Pressed: true, Modifiers: experience.ModControl}
	m.Send(v.id, key)
	if text, ok := m.TakeCopy(); !ok || text != "測定" {
		t.Fatal("annotation copy failed", text)
	}
	key.Keycode = 47
	m.Send(v.id, key)
	if !m.TakePasteRequest() {
		t.Fatal("annotation paste missing")
	}
	m.Focus(0)
	m.Focus(v.id)
	m.Paste("late text")
	if v.note.Text() != "測定" {
		t.Fatal("paste survived focus loss")
	}
	m.Send(v.id, key)
	if !m.TakePasteRequest() {
		t.Fatal("new paste blocked")
	}
	m.CloseApplication(v.id)
	m.Paste("closed")
	if v.note.Text() != "測定" {
		t.Fatal("paste reached a closed model")
	}
}
