package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/codemodify/worldr/internal/mediaapp"
	"github.com/codemodify/worldr/internal/modelapp"
	"github.com/codemodify/worldr/internal/nativeapps"
	"github.com/codemodify/worldr/internal/noteapp"
	"github.com/codemodify/worldr/internal/photoapp"
	"github.com/codemodify/worldr/internal/projectapp"
	"github.com/codemodify/worldr/internal/researchapp"
	"github.com/codemodify/worldr/internal/resourcepath"
	"github.com/codemodify/worldr/internal/workspace"
)

// A manifest describes reopenable resources and bounded inert terminal task
// recipes. Recipes are text that requires a new explicit stage/run action after
// restore; the manifest never stores an instruction to execute, process state,
// arbitrary executables, environment variables, descriptors or identities.
type sessionManifest struct {
	Photos    []photoapp.WindowState           `json:"photos,omitempty"`
	Videos    []mediaapp.WindowState           `json:"videos,omitempty"`
	Models    []modelapp.SessionState          `json:"models,omitempty"`
	Research  []researchapp.SessionState       `json:"research,omitempty"`
	Notes     []noteapp.SessionState           `json:"notes,omitempty"`
	Version   int                              `json:"version"`
	Project   *projectapp.SessionState         `json:"project,omitempty"`
	Terminals []nativeapps.TerminalState       `json:"terminals,omitempty"`
	Photo     *photoapp.SessionState           `json:"photo,omitempty"`
	Media     *mediaapp.SessionState           `json:"media,omitempty"`
	Axial     *workspace.AxialApplicationState `json:"axial,omitempty"`
}

func (s *sessionManifest) validate() error {
	if s == nil || s.Version != 1 {
		return fmt.Errorf("unsupported session version")
	}
	if len(s.Models) > modelapp.MaxViewers {
		return fmt.Errorf("session exceeds model window budget")
	}
	if len(s.Research) > researchapp.MaxDashboards {
		return fmt.Errorf("session exceeds research dashboard budget")
	}
	if len(s.Notes) > noteapp.MaxNotes {
		return fmt.Errorf("session exceeds native note budget")
	}
	count := len(s.Terminals) + len(s.Models) + len(s.Research) + len(s.Notes) + len(s.Photos) + len(s.Videos)
	if s.Project != nil {
		count++
		if err := s.Project.Validate(); err != nil {
			return fmt.Errorf("Files: %w", err)
		}
	}
	if s.Photo != nil {
		count++
		if err := resourcepath.Validate(s.Photo.Path); err != nil {
			return fmt.Errorf("photo: %w", err)
		}
	}
	if s.Media != nil {
		count++
		if err := s.Media.Validate(); err != nil {
			return fmt.Errorf("video: %w", err)
		}
	}
	if s.Axial != nil {
		count++
		if err := s.Axial.Validate(); err != nil {
			return err
		}
	}
	if count > 32 {
		return fmt.Errorf("session exceeds the 32-window limit")
	}
	photoCount, videoCount := len(s.Photos), len(s.Videos)
	keys := make(map[string]bool)
	if s.Axial != nil {
		keys[s.Axial.Key] = true
	}
	if s.Photo != nil {
		photoCount++
		keys["native:photo-viewer"] = true
	}
	if s.Media != nil {
		videoCount++
		keys["native:media-player"] = true
	}
	if photoCount > photoapp.MaxViewers || videoCount > mediaapp.MaxViewers {
		return fmt.Errorf("session exceeds photo/video window budgets")
	}
	for _, state := range s.Photos {
		if err := state.Validate(); err != nil {
			return err
		}
		if keys[state.Key] {
			return fmt.Errorf("duplicate photo key %q", state.Key)
		}
		keys[state.Key] = true
	}
	for _, state := range s.Videos {
		if err := state.Validate(); err != nil {
			return err
		}
		if keys[state.Key] {
			return fmt.Errorf("duplicate video key %q", state.Key)
		}
		keys[state.Key] = true
	}
	for _, model := range s.Models {
		if err := model.Validate(); err != nil {
			return err
		}
		if keys[model.Key] {
			return fmt.Errorf("duplicate model key %q", model.Key)
		}
		keys[model.Key] = true
	}
	for _, dashboard := range s.Research {
		if err := dashboard.Validate(); err != nil {
			return err
		}
		if keys[dashboard.Key] {
			return fmt.Errorf("duplicate research key %q", dashboard.Key)
		}
		keys[dashboard.Key] = true
	}
	for _, note := range s.Notes {
		if err := note.Validate(); err != nil {
			return err
		}
		if keys[note.Key] {
			return fmt.Errorf("duplicate native note key %q", note.Key)
		}
		keys[note.Key] = true
	}
	for _, terminal := range s.Terminals {
		if err := terminal.Validate(); err != nil {
			return err
		}
		if keys[terminal.Key] {
			return fmt.Errorf("duplicate terminal key %q", terminal.Key)
		}
		keys[terminal.Key] = true
	}
	return nil
}

func decodeSessionManifest(raw []byte) (*sessionManifest, error) {
	// Duplicate fields cannot silently replace a saved file or working folder.
	if err := rejectDuplicateFields(json.NewDecoder(bytes.NewReader(raw)), 0); err != nil {
		return nil, err
	}
	var result *sessionManifest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&result); err != nil {
		return nil, err
	}
	if err := result.validate(); err != nil {
		return nil, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("session requires one JSON value")
	}
	return result, nil
}

func rejectDuplicateFields(d *json.Decoder, depth int) error {
	if depth > 12 {
		return fmt.Errorf("session nesting is too deep")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		seen := make(map[string]bool)
		for d.More() {
			token, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok || seen[key] {
				return fmt.Errorf("invalid or duplicate session field %q", key)
			}
			seen[key] = true
			if err := rejectDuplicateFields(d, depth+1); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	case json.Delim('['):
		for d.More() {
			if err := rejectDuplicateFields(d, depth+1); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	}
	return nil
}
