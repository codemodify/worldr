package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
)

type persistenceFixture struct {
	value   int
	invalid bool
	saveErr error
}

func (f *persistenceFixture) Info() experience.Info {
	return experience.Info{ID: "test.experience", Title: "Fixture"}
}
func (f *persistenceFixture) Atlas() render.Atlas          { return render.Atlas{} }
func (f *persistenceFixture) Update(time.Duration)         {}
func (f *persistenceFixture) Draw(int, int) render.Frame   { return render.Frame{} }
func (f *persistenceFixture) Handle(experience.Event) bool { return false }
func (f *persistenceFixture) Close() error                 { return nil }
func (f *persistenceFixture) SaveState() ([]byte, error) {
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	if f.invalid {
		return []byte(`{invalid`), nil
	}
	return json.Marshal(struct {
		Value int `json:"value"`
	}{f.value})
}
func (f *persistenceFixture) LoadState(data []byte) error {
	var next struct {
		Value int `json:"value"`
	}
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	if next.Value < 0 {
		return errors.New("invalid fixture value")
	}
	f.value = next.Value
	return nil
}

func TestStateRoundTripCreatesPrivateAtomicFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "explicit", "nested", "workspace.json")
	f := &persistenceFixture{value: 42}
	if err := loadState(path, f); err != nil {
		t.Fatal("missing file must start a fresh experience", err)
	}
	if err := saveState(path, f); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("state permissions=%v", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := decodeStateEnvelope(data)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.ExperienceID != "test.experience" || envelope.Version != 1 {
		t.Fatal(envelope)
	}
	f.value = 0
	if err := loadState(path, f); err != nil || f.value != 42 {
		t.Fatalf("reload value=%d, error=%v", f.value, err)
	}
	f.value = 99
	if err := saveState(path, f); err != nil {
		t.Fatal(err)
	}
	f.value = 0
	if err := loadState(path, f); err != nil || f.value != 99 {
		t.Fatalf("replacement reload=%d,error=%v", f.value, err)
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatal("temporary files were left behind", files)
	}
}

func TestStateEnvelopeValidationPreservesExperience(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.json")
	for _, data := range []string{
		`{"version":1,"experience_id":"different","state":{"value":20}}`,
		`{"version":2,"experience_id":"test.experience","state":{"value":20}}`,
		`{"version":1,"experience_id":"test.experience","state":{"value":20},"extra":true}`,
		`{"version":1,"version":1,"experience_id":"test.experience","state":{"value":20}}`,
		`{"version":1,"experience_id":"test.experience"}`,
		`{"version":1,"experience_id":"test.experience","state":{"value":20}} {}`,
		`{"version":1,"experience_id":"test.experience","state":{"value":-1}}`,
		`{"version":`,
		`null`,
	} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		f := &persistenceFixture{value: 7}
		if err := loadState(path, f); err == nil {
			t.Fatalf("accepted invalid envelope %s", data)
		}
		if f.value != 7 {
			t.Fatal("failed load changed live state")
		}
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte(" "), maxStateBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if err := loadState(path, &persistenceFixture{}); err == nil {
		t.Fatal("accepted oversized state file")
	}
}

func TestFailedSavePreservesPriorFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.json")
	f := &persistenceFixture{value: 12}
	if err := saveState(path, f); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, broken := range []*persistenceFixture{{invalid: true}, {saveErr: errors.New("encoder failed")}} {
		if err := saveState(path, broken); err == nil {
			t.Fatal("invalid experience state was saved")
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("failed save modified the prior atomic file")
		}
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatal("failed saves leaked temporary files")
	}
}

func TestPersistenceCapabilityIsRequiredOnlyWhenConfigured(t *testing.T) {
	stateless := struct{ experience.Experience }{&persistenceFixture{}}
	if err := loadState("", stateless); err != nil {
		t.Fatal(err)
	}
	if err := saveState("", stateless); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "workspace.json")
	if err := loadState(path, stateless); err == nil {
		t.Fatal("configured load accepted a stateless experience")
	}
	if err := saveState(path, stateless); err == nil {
		t.Fatal("configured save accepted a stateless experience")
	}
}
