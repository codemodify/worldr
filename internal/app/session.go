package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/codemodify/worldr/internal/experience"
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

type workspaceSession struct {
	models    *modelapp.Manager
	research  *researchapp.Manager
	notes     *noteapp.Manager
	axial     *workspace.AxialApplicationManager
	work      experience.Experience
	project   *projectapp.Provider
	terminals *nativeapps.Manager
	photos    fileViewer
	media     fileViewer
	pending   sessionManifest // Unavailable resources keep their saved associations.
}

func (s *workspaceSession) snapshot() *sessionManifest {
	s.reconcile()
	result := &sessionManifest{Version: 1}
	keys := map[string]bool{}
	if layouts, ok := s.work.(interface{ SavedApplicationKeys() []string }); ok {
		for _, key := range layouts.SavedApplicationKeys() {
			keys[key] = true
		}
	}
	if s.project != nil {
		if state, ok := s.project.SessionState(); ok {
			result.Project = &state
		}
	}
	if result.Project == nil && keys["native:project-browser"] {
		result.Project = s.pending.Project
	}
	for _, state := range photoStates(s.photos) {
		if state.Key == "native:photo-viewer" {
			copy := state.SessionState
			result.Photo = &copy
		} else {
			result.Photos = append(result.Photos, state)
		}
		delete(keys, state.Key)
	}
	if result.Photo == nil && keys["native:photo-viewer"] {
		result.Photo = s.pending.Photo
	}
	for _, state := range s.pending.Photos {
		if keys[state.Key] {
			result.Photos = append(result.Photos, state)
		}
	}
	for _, state := range videoStates(s.media) {
		if state.Key == "native:media-player" {
			copy := state.SessionState
			result.Media = &copy
		} else {
			result.Videos = append(result.Videos, state)
		}
		delete(keys, state.Key)
	}
	if result.Media == nil && keys["native:media-player"] {
		result.Media = s.pending.Media
	}
	for _, state := range s.pending.Videos {
		if keys[state.Key] {
			result.Videos = append(result.Videos, state)
		}
	}
	if s.terminals != nil {
		result.Terminals = s.terminals.SessionTerminals()
	}
	for _, state := range result.Terminals {
		delete(keys, state.Key)
	}
	for _, state := range s.pending.Terminals {
		if keys[state.Key] {
			result.Terminals = append(result.Terminals, state)
		}
	}
	if s.models != nil {
		result.Models = s.models.SessionStates()
	}
	for _, state := range result.Models {
		delete(keys, state.Key)
	}
	for _, state := range s.pending.Models {
		if keys[state.Key] {
			result.Models = append(result.Models, state)
		}
	}
	if s.research != nil {
		result.Research = s.research.SessionStates()
	}
	for _, state := range result.Research {
		delete(keys, state.Key)
	}
	for _, state := range s.pending.Research {
		if keys[state.Key] {
			result.Research = append(result.Research, state)
		}
	}
	if s.notes != nil {
		result.Notes = s.notes.SessionStates()
	}
	for _, state := range result.Notes {
		delete(keys, state.Key)
	}
	for _, state := range s.pending.Notes {
		if keys[state.Key] {
			result.Notes = append(result.Notes, state)
		}
	}
	if s.axial != nil {
		if states := s.axial.SessionStates(); len(states) == 1 {
			state := states[0]
			result.Axial = &state
			delete(keys, state.Key)
		}
	}
	if result.Axial == nil && keys[workspace.AxialApplicationKey] {
		result.Axial = s.pending.Axial
	}
	return result
}

// A successful reopen replaces a previously unavailable association. Observe
// this after each input event and poll, so opening then closing a replacement
// between checkpoints cannot resurrect the old resource on the next startup.
func (s *workspaceSession) reconcile() {
	if s.models != nil && len(s.pending.Models) != 0 {
		live := map[string]bool{}
		for _, surface := range s.models.Surfaces() {
			live[surface.Key] = true
		}
		remaining := s.pending.Models[:0]
		for _, state := range s.pending.Models {
			if !live[state.Key] {
				remaining = append(remaining, state)
			}
		}
		s.pending.Models = remaining
	}
	if s.research != nil && len(s.pending.Research) != 0 {
		live := map[string]bool{}
		for _, surface := range s.research.Surfaces() {
			live[surface.Key] = true
		}
		remaining := s.pending.Research[:0]
		for _, state := range s.pending.Research {
			if !live[state.Key] {
				remaining = append(remaining, state)
			}
		}
		s.pending.Research = remaining
	}
	if s.notes != nil && len(s.pending.Notes) != 0 {
		live := map[string]bool{}
		for _, surface := range s.notes.Surfaces() {
			live[surface.Key] = true
		}
		remaining := s.pending.Notes[:0]
		for _, state := range s.pending.Notes {
			if !live[state.Key] {
				remaining = append(remaining, state)
			}
		}
		s.pending.Notes = remaining
	}
	if s.project != nil && len(s.project.Surfaces()) != 0 {
		s.pending.Project = nil
	}
	if s.photos != nil {
		live := map[string]bool{}
		for _, surface := range s.photos.Surfaces() {
			live[surface.Key] = true
		}
		if live["native:photo-viewer"] {
			s.pending.Photo = nil
		}
		remaining := s.pending.Photos[:0]
		for _, state := range s.pending.Photos {
			if !live[state.Key] {
				remaining = append(remaining, state)
			}
		}
		s.pending.Photos = remaining
	}
	if s.media != nil {
		live := map[string]bool{}
		for _, surface := range s.media.Surfaces() {
			live[surface.Key] = true
		}
		if live["native:media-player"] {
			s.pending.Media = nil
		}
		remaining := s.pending.Videos[:0]
		for _, state := range s.pending.Videos {
			if !live[state.Key] {
				remaining = append(remaining, state)
			}
		}
		s.pending.Videos = remaining
	}
	if s.terminals != nil && len(s.pending.Terminals) != 0 {
		live := make(map[string]bool)
		for _, surface := range s.terminals.Surfaces() {
			live[surface.Key] = true
		}
		remaining := s.pending.Terminals[:0]
		for _, state := range s.pending.Terminals {
			if !live[state.Key] {
				remaining = append(remaining, state)
			}
		}
		s.pending.Terminals = remaining
	}
	if s.axial != nil && len(s.axial.Surfaces()) != 0 {
		s.pending.Axial = nil
	}
}

func (s *workspaceSession) save(path string, writer *autosaver) error {
	if path == "" {
		return nil
	}
	// Finish an older checkpoint before replacing the primary document, so it
	// cannot later masquerade as a newer crash-recovery snapshot.
	_ = writer.Flush()
	data, err := encodeState(s.work, s.snapshot(), false)
	if err != nil {
		return err
	}
	if err := writeState(path, data); err != nil {
		return err
	}
	writer.Reset(data)
	if err := os.Remove(recoveryPath(path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("workspace saved, but recovery cleanup failed: %w", err)
	}
	return nil
}

func notifyWorkspace(work experience.Experience, message string) {
	if receiver, ok := work.(experience.NotificationReceiver); ok {
		receiver.Notify(message)
	}
}

// Explicit roots override a saved root. Repeating the same root on the command
// line still resumes its saved subfolder and selection.
func openSessionProject(explicit string, saved *projectapp.SessionState) (*projectapp.Provider, error) {
	if explicit == "" {
		if saved == nil {
			return nil, nil
		}
		return projectapp.NewSession(*saved)
	}
	if saved != nil {
		absolute, err := filepath.Abs(explicit)
		if err == nil {
			absolute, err = filepath.EvalSymlinks(absolute)
		}
		if err == nil && absolute == saved.Root {
			return projectapp.NewSession(*saved)
		}
	}
	return projectapp.New(explicit)
}

func checkSessionPlacement(work experience.Experience, hub *applicationHub, key string) error {
	if hub == nil || hub.closed {
		return fmt.Errorf("application hosting is unavailable")
	}
	live := false
	for _, surface := range hub.Surfaces() {
		live = live || surface.Key == key
	}
	if !live && len(hub.Surfaces()) >= 32 {
		return fmt.Errorf("workspace has reached its 32-window limit")
	}
	// At startup the experience is not attached yet. Reserve the union of
	// remembered keys and surfaces already restored into the hub, not just
	// the saved layout's currently empty slots.
	if layouts, ok := work.(interface{ SavedApplicationKeys() []string }); ok {
		keys := make(map[string]bool)
		for _, saved := range layouts.SavedApplicationKeys() {
			keys[saved] = true
		}
		for _, surface := range hub.Surfaces() {
			keys[surface.Key] = true
		}
		if !keys[key] && len(keys) >= 32 {
			return fmt.Errorf("saved layout has reached its 32-window limit")
		}
	}
	if checker, ok := work.(experience.ApplicationPlacementChecker); ok {
		return checker.CheckApplicationPlacement(key)
	}
	return nil
}

// Restore is performed before connecting providers to the experience. Every
// requested surface is published before initial focus/camera reconciliation.
// Failures are independent and do not erase the resource's saved placement.
func (s *workspaceSession) restore(saved *sessionManifest, hub *applicationHub, out io.Writer) []error {
	if saved == nil {
		return nil
	}
	var failures []error
	report := func(kind, path string, err error) {
		failure := fmt.Errorf("%s %q: %w", kind, path, err)
		failures = append(failures, failure)
		fmt.Fprintf(out, "session restore: %v\n", failure)
	}
	if saved.Axial != nil {
		state := *saved.Axial
		err := checkSessionPlacement(s.work, hub, state.Key)
		if err == nil && s.axial == nil {
			err = fmt.Errorf("hosted AXIAL is unavailable")
		}
		if err == nil {
			_, err = s.axial.Restore(state)
		}
		if err != nil {
			s.pending.Axial = &state
			report("hosted AXIAL", state.Key, err)
		}
	}
	for _, state := range saved.Models {
		err := checkSessionPlacement(s.work, hub, state.Key)
		if err == nil && s.models == nil {
			err = fmt.Errorf("model inspector is unavailable")
		}
		if err == nil {
			_, err = s.models.Restore(state)
		}
		if err != nil {
			s.pending.Models = append(s.pending.Models, state)
			report("model", state.ResourcePath(), err)
		}
	}
	for _, state := range saved.Research {
		err := checkSessionPlacement(s.work, hub, state.Key)
		if err == nil && s.research == nil {
			err = fmt.Errorf("research workbench is unavailable")
		}
		if err == nil {
			_, err = s.research.Restore(state)
		}
		if err != nil {
			s.pending.Research = append(s.pending.Research, state)
			report("research dataset", state.Source, err)
		}
	}
	for _, state := range saved.Notes {
		err := checkSessionPlacement(s.work, hub, state.Key)
		if err == nil && s.notes == nil {
			err = fmt.Errorf("native note editor is unavailable")
		}
		if err == nil {
			_, err = s.notes.Restore(state)
		}
		if err != nil {
			s.pending.Notes = append(s.pending.Notes, state)
			label := state.Source
			if label == "" {
				label = "untitled"
			}
			report("native note", label, err)
		}
	}
	for _, state := range saved.Terminals {
		err := checkSessionPlacement(s.work, hub, state.Key)
		if err == nil && s.terminals == nil {
			err = fmt.Errorf("native terminals are unavailable")
		}
		if err == nil {
			directory, openErr := resourcepath.OpenDirectory(state.Directory)
			err = openErr
			if err == nil {
				_, err = s.terminals.RestoreTerminalState(state, directory)
				_ = directory.Close()
			}
		}
		if err != nil {
			s.pending.Terminals = append(s.pending.Terminals, state)
			report("terminal folder", state.Directory, err)
		}
	}
	photos := append([]photoapp.WindowState(nil), saved.Photos...)
	if saved.Photo != nil {
		photos = append([]photoapp.WindowState{{Key: "native:photo-viewer", SessionState: *saved.Photo}}, photos...)
	}
	for _, state := range photos {
		err := checkSessionPlacement(s.work, hub, state.Key)
		if err == nil && s.photos == nil {
			err = fmt.Errorf("photo viewer is unavailable")
		}
		if err == nil {
			file, openErr := resourcepath.OpenFile(state.Path)
			err = openErr
			if err == nil {
				if collection, ok := s.photos.(interface {
					RestoreFile(*os.File, string, photoapp.WindowState) (string, error)
				}); ok {
					_, err = collection.RestoreFile(file, filepath.Base(state.Path), state)
				} else if state.Key == "native:photo-viewer" {
					_, err = s.photos.OpenFile(file, filepath.Base(state.Path))
				} else {
					err = fmt.Errorf("multiple photo restoration is unavailable")
				}
				if err != nil {
					file.Close()
				}
			}
		}
		if err != nil {
			if state.Key == "native:photo-viewer" {
				copy := state.SessionState
				s.pending.Photo = &copy
			} else {
				s.pending.Photos = append(s.pending.Photos, state)
			}
			report("photo", state.Path, err)
		}
	}
	videos := append([]mediaapp.WindowState(nil), saved.Videos...)
	if saved.Media != nil {
		videos = append([]mediaapp.WindowState{{Key: "native:media-player", SessionState: *saved.Media}}, videos...)
	}
	for _, state := range videos {
		err := checkSessionPlacement(s.work, hub, state.Key)
		if err == nil && s.media == nil {
			err = fmt.Errorf("media player is unavailable")
		}
		if err == nil {
			file, openErr := resourcepath.OpenFile(state.Path)
			err = openErr
			if err == nil {
				if collection, ok := s.media.(interface {
					RestoreFile(*os.File, string, mediaapp.WindowState) (string, error)
				}); ok {
					_, err = collection.RestoreFile(file, filepath.Base(state.Path), state)
				} else if manager, ok := s.media.(*mediaapp.Manager); ok && state.Key == "native:media-player" {
					_, err = manager.RestoreFile(file, filepath.Base(state.Path), state.SessionState)
				} else {
					err = fmt.Errorf("multiple video restoration is unavailable")
				}
				if err != nil {
					file.Close()
				}
			}
		}
		if err != nil {
			if state.Key == "native:media-player" {
				copy := state.SessionState
				s.pending.Media = &copy
			} else {
				s.pending.Videos = append(s.pending.Videos, state)
			}
			report("video", state.Path, err)
		}
	}
	return failures
}

func photoStates(viewer fileViewer) []photoapp.WindowState {
	if collection, ok := viewer.(interface{ SessionStates() []photoapp.WindowState }); ok {
		return collection.SessionStates()
	}
	if manager, ok := viewer.(*photoapp.Manager); ok {
		if state, valid := manager.SessionState(); valid {
			return []photoapp.WindowState{{Key: "native:photo-viewer", SessionState: state}}
		}
	}
	return nil
}
func videoStates(viewer fileViewer) []mediaapp.WindowState {
	if collection, ok := viewer.(interface{ SessionStates() []mediaapp.WindowState }); ok {
		return collection.SessionStates()
	}
	if manager, ok := viewer.(*mediaapp.Manager); ok {
		if state, valid := manager.SessionState(); valid {
			return []mediaapp.WindowState{{Key: "native:media-player", SessionState: state}}
		}
	}
	return nil
}
