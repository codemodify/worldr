package modelapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/resourcepath"
	"github.com/codemodify/worldr/internal/scene"
)

type Measurement struct {
	A, B     scene.Vec3
	Distance float32 `json:"distance"`
}
type Annotation struct {
	Point scene.Vec3 `json:"point"`
	Text  string     `json:"text"`
}
type ViewState struct {
	Yaw          float32       `json:"yaw"`
	Pitch        float32       `json:"pitch"`
	Zoom         float32       `json:"zoom"`
	Selected     int           `json:"selected"`
	Units        string        `json:"units"`
	Measurements []Measurement `json:"measurements,omitempty"`
	Annotations  []Annotation  `json:"annotations,omitempty"`
}
type SessionState struct {
	Key          string    `json:"key"`
	DocumentOnly bool      `json:"document_only,omitempty"`
	Source       string    `json:"source"`
	Document     string    `json:"document"`
	View         ViewState `json:"view"`
}
type toolDocument struct {
	Version int       `json:"version"`
	Source  string    `json:"source"`
	View    ViewState `json:"view"`
}

func slotKey(slot int) string {
	if slot == 0 {
		return "native:model"
	}
	return fmt.Sprintf("native:model-%d", slot+1)
}
func keySlot(key string) (int, error) {
	for i := 0; i < 32; i++ {
		if slotKey(i) == key {
			return i, nil
		}
	}
	return -1, fmt.Errorf("invalid model viewer key")
}
func validNumber(n float32) bool {
	return !math.IsNaN(float64(n)) && !math.IsInf(float64(n), 0) && math.Abs(float64(n)) <= 1e12
}
func validPoint(p scene.Vec3) bool { return validNumber(p.X) && validNumber(p.Y) && validNumber(p.Z) }
func (v ViewState) Validate() error {
	if !validNumber(v.Yaw) || !validNumber(v.Pitch) || !validNumber(v.Zoom) || v.Zoom < .2 || v.Zoom > 4 || v.Selected < 0 || v.Selected >= 256 {
		return fmt.Errorf("invalid model view")
	}
	if v.Units != "units" && v.Units != "mm" && v.Units != "cm" && v.Units != "m" && v.Units != "in" {
		return fmt.Errorf("unsupported model units")
	}
	if len(v.Measurements) > 128 || len(v.Annotations) > 128 {
		return fmt.Errorf("model document annotation budget exceeded")
	}
	for _, m := range v.Measurements {
		if !validPoint(m.A) || !validPoint(m.B) || !validNumber(m.Distance) || m.Distance < 0 {
			return fmt.Errorf("invalid measurement")
		}
		distance := m.B.Sub(m.A).Length()
		if !validNumber(distance) || math.Abs(float64(distance-m.Distance)) > math.Max(1e-4, float64(distance)*1e-5) {
			return fmt.Errorf("measurement does not match its endpoints")
		}
	}
	for _, a := range v.Annotations {
		if !validPoint(a.Point) || a.Text == "" || len(a.Text) > 512 || !utf8.ValidString(a.Text) {
			return fmt.Errorf("invalid annotation")
		}
	}
	return nil
}
func (s SessionState) Validate() error {
	if _, err := keySlot(s.Key); err != nil {
		return err
	}
	if s.DocumentOnly {
		if s.Source != "" {
			return fmt.Errorf("pending model document cannot also specify a source")
		}
	} else if err := resourcepath.Validate(s.Source); err != nil {
		return err
	}
	if err := resourcepath.Validate(s.Document); err != nil {
		return err
	}
	if s.Document == s.Source || !strings.HasSuffix(strings.ToLower(s.Document), ".worldr-model.json") {
		return fmt.Errorf("model notes require a separate .worldr-model.json document")
	}
	if ext := strings.ToLower(filepath.Ext(s.Source)); !s.DocumentOnly && ext != ".obj" && ext != ".stl" {
		return fmt.Errorf("model source must be OBJ or STL")
	}
	return s.View.Validate()
}
func (s SessionState) ResourcePath() string {
	if s.DocumentOnly {
		return s.Document
	}
	return s.Source
}
func defaultView() ViewState { return ViewState{Yaw: -.5, Pitch: .5, Zoom: 1, Units: "units"} }
func cloneView(v ViewState) ViewState {
	v.Measurements = append([]Measurement(nil), v.Measurements...)
	v.Annotations = append([]Annotation(nil), v.Annotations...)
	return v
}
func readDocument(file *os.File) (toolDocument, error) {
	var d toolDocument
	data, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return d, err
	}
	if len(data) > 1<<20 {
		return d, fmt.Errorf("tool document exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return d, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return d, fmt.Errorf("tool document requires one JSON object")
	}
	if d.Version != 1 {
		return d, fmt.Errorf("unsupported model document version")
	}
	if err := resourcepath.Validate(d.Source); err != nil {
		return d, err
	}
	return d, d.View.Validate()
}
func writeDocument(state SessionState) error {
	if state.DocumentOnly {
		return fmt.Errorf("model document is still loading")
	}
	if err := state.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(toolDocument{1, state.Source, state.View}, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(state.Document), ".worldr-model-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	defer file.Close()
	if _, err = file.Write(append(data, '\n')); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, state.Document); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(state.Document))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
