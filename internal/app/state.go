package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
)

const maxStateBytes = 1 << 20
const stateEnvelopeVersion = 1
const sessionEnvelopeVersion = 2

type stateEnvelope struct {
	Version      int              `json:"version"`
	ExperienceID string           `json:"experience_id"`
	State        json.RawMessage  `json:"state"`
	Session      *sessionManifest `json:"session,omitempty"`
}

func persistentExperience(exp experience.Experience) (experience.Stateful, error) {
	stateful, ok := exp.(experience.Stateful)
	if !ok {
		return nil, fmt.Errorf("experience %q does not support persistence", exp.Info().ID)
	}
	if exp.Info().ID == "" {
		return nil, fmt.Errorf("persistent experience needs a stable ID")
	}
	return stateful, nil
}

func loadState(path string, exp experience.Experience) error {
	_, err := loadSessionState(path, exp)
	return err
}

func loadSessionState(path string, exp experience.Experience) (*sessionManifest, error) {
	if path == "" {
		return nil, nil
	}
	stateful, err := persistentExperience(exp)
	if err != nil {
		return nil, err
	}
	f, err := openStateFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open state: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxStateBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	if len(data) > maxStateBytes {
		return nil, fmt.Errorf("state file exceeds %d bytes", maxStateBytes)
	}
	envelope, err := decodeStateEnvelope(data)
	if err != nil {
		return nil, err
	}
	if envelope.Version != stateEnvelopeVersion && envelope.Version != sessionEnvelopeVersion {
		return nil, fmt.Errorf("unsupported state envelope version %d", envelope.Version)
	}
	if envelope.Version == stateEnvelopeVersion && envelope.Session != nil || envelope.Version == sessionEnvelopeVersion && envelope.Session == nil {
		return nil, fmt.Errorf("state envelope version does not match session payload")
	}
	if envelope.Session != nil {
		if err := envelope.Session.validate(); err != nil {
			return nil, fmt.Errorf("invalid saved session: %w", err)
		}
	}
	if envelope.ExperienceID != exp.Info().ID {
		return nil, fmt.Errorf("state belongs to experience %q, expected %q", envelope.ExperienceID, exp.Info().ID)
	}
	if err := stateful.LoadState(envelope.State); err != nil {
		return nil, fmt.Errorf("load experience state: %w", err)
	}
	return envelope.Session, nil
}

// Decode field-by-field to reject duplicate envelope keys as well as unknown
// keys and trailing values. The nested state belongs to the experience schema.
func decodeStateEnvelope(data []byte) (stateEnvelope, error) {
	var result stateEnvelope
	if !utf8.Valid(data) {
		return result, fmt.Errorf("state envelope must be valid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return result, fmt.Errorf("state envelope must be a JSON object")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return result, fmt.Errorf("decode state envelope: %w", err)
		}
		name, ok := token.(string)
		if !ok {
			return result, fmt.Errorf("invalid state envelope key")
		}
		if seen[name] {
			return result, fmt.Errorf("duplicate state envelope key %q", name)
		}
		seen[name] = true
		switch name {
		case "version":
			err = d.Decode(&result.Version)
		case "experience_id":
			err = d.Decode(&result.ExperienceID)
		case "state":
			err = d.Decode(&result.State)
		case "session":
			var raw json.RawMessage
			if err = d.Decode(&raw); err == nil {
				result.Session, err = decodeSessionManifest(raw)
			}
		default:
			return result, fmt.Errorf("unknown state envelope key %q", name)
		}
		if err != nil {
			return result, fmt.Errorf("decode state envelope %s: %w", name, err)
		}
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return result, fmt.Errorf("invalid state envelope closing delimiter")
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return result, fmt.Errorf("state envelope must contain exactly one JSON value")
	}
	if !seen["version"] || !seen["experience_id"] || !seen["state"] {
		return result, fmt.Errorf("state envelope requires version, experience_id, and state")
	}
	return result, nil
}

func saveState(path string, exp experience.Experience) error {
	return saveSessionState(path, exp, nil)
}

func saveSessionState(path string, exp experience.Experience, session *sessionManifest) error {
	if path == "" {
		return nil
	}
	data, err := encodeState(exp, session, false)
	if err != nil {
		return err
	}
	return writeState(path, data)
}

func encodeState(exp experience.Experience, session *sessionManifest, checkpoint bool) ([]byte, error) {
	stateful, err := persistentExperience(exp)
	if err != nil {
		return nil, err
	}
	var payload []byte
	if checkpoint {
		snapshotter, ok := exp.(experience.Checkpointer)
		if !ok {
			return nil, fmt.Errorf("experience does not support non-disruptive checkpoints")
		}
		payload, err = snapshotter.CheckpointState()
	} else {
		payload, err = stateful.SaveState()
	}
	if err != nil {
		return nil, fmt.Errorf("save experience state: %w", err)
	}
	if len(payload) > maxStateBytes {
		return nil, fmt.Errorf("experience state exceeds %d bytes", maxStateBytes)
	}
	if !json.Valid(payload) {
		return nil, fmt.Errorf("experience returned invalid JSON state")
	}
	version := stateEnvelopeVersion
	if session != nil {
		if err := session.validate(); err != nil {
			return nil, fmt.Errorf("save session: %w", err)
		}
		version = sessionEnvelopeVersion
	}
	data, err := json.MarshalIndent(stateEnvelope{Version: version, ExperienceID: exp.Info().ID, State: payload, Session: session}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode state envelope: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxStateBytes {
		return nil, fmt.Errorf("state envelope exceeds %d bytes", maxStateBytes)
	}
	return data, nil
}

func writeState(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	dir, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open state directory: %w", err)
	}
	defer dir.Close()
	tmp, err := os.CreateTemp(directory, "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	temporaryPath := tmp.Name()
	defer func() {
		tmp.Close()
		if temporaryPath != "" {
			os.Remove(temporaryPath)
		}
	}()
	if err = tmp.Chmod(0600); err != nil {
		return fmt.Errorf("set state permissions: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync state: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close state: %w", err)
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	temporaryPath = ""
	if err = dir.Sync(); err != nil {
		return fmt.Errorf("sync state directory after replacement: %w", err)
	}
	return nil
}
