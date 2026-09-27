package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/codemodify/worldr/internal/experience"
)

type experimentDefinition struct {
	ID, Title, Description                    string
	experience, skin, client, state, template string
	width, height                             int
}

// Order follows the studies' progression. Keep Workspace Navigator last.
func experimentCatalog() []experimentDefinition {
	return []experimentDefinition{
		{ID: "axial", Title: "AXIAL / 07", Description: "Original 3D engineering study", experience: "axial", state: "dist/experiments/axial.json", width: 1440, height: 900},
		{ID: "workspace", Title: "Spatial Workspace", Description: "Free placement, windows and desktop tools", experience: "workspace", state: "dist/experiments/workspace.json", width: 1440, height: 900},
		{ID: "skin-studio", Title: "Skin Studio", Description: "Skinnable controls and window frames", experience: "workspace", skin: "merrick", client: "skin-studio", state: "dist/skin-studio.json", width: 1280, height: 820},
		{ID: "merrick", Title: "Merrick Desktop", Description: "Sculpted chrome and modular panels", experience: "workspace", skin: "merrick", client: "merrick-desktop", state: "dist/merrick-desktop/state.json", template: "examples/merrick-desktop/layout.json", width: 1280, height: 560},
		{ID: "advanced", Title: "Advanced Studio", Description: "Steel-blue navigation and inset controls", experience: "workspace", skin: "advanced", client: "advanced-desktop", state: "dist/advanced-desktop/state.json", template: "examples/advanced-desktop/layout.json", width: 900, height: 740},
		{ID: "hologram", Title: "Hologram Disk Management", Description: "Neon frames and dimensional disk blocks", experience: "workspace", skin: "hologram", client: "hologram-desktop", state: "dist/hologram-desktop/state.json", template: "examples/hologram-desktop/layout.json", width: 1000, height: 790},
		{ID: "plasma", Title: "Plasma Fluid Surfaces", Description: "Joining glass panels and spring motion", experience: "plasma", state: "dist/plasma/playground.json", width: 884, height: 720},
		{ID: "future-panels", Title: "Future Panels", Description: "Layered glass applications with spatial motion", experience: "workspace", skin: "future-panels", state: "dist/future-panels/workspace.json", template: "examples/future-panels/workspace.json", width: 1440, height: 900},
		{ID: "navigator", Title: "Workspace Navigator", Description: "Projects, app previews and focused work", experience: "navigator", state: "dist/navigator/workspace.json", template: "examples/navigator-desktop/workspace.json", width: 1280, height: 820},
	}
}

func currentExperiment(o Options) string {
	if o.experimentID != "" {
		return o.experimentID
	}
	if o.Experience == "navigator" || o.Experience == "plasma" || o.Experience == "axial" {
		return o.Experience
	}
	for _, def := range experimentCatalog() {
		for _, command := range o.NativeApplications {
			if def.client != "" && filepath.Base(command) == def.client {
				return def.ID
			}
		}
	}
	if o.Skin == "future-panels" {
		return "future-panels"
	}
	return "workspace"
}

// RunExperiments retains launch settings for visited sessions. Every switch
// passes through Run's save and teardown before another presenter is created.
func RunExperiments(ctx context.Context, out io.Writer, o Options, factory func(Options) (experience.Experience, error)) error {
	if o.Version || o.ListDevices || o.ListOutputs {
		return Run(ctx, out, o, func() (experience.Experience, error) { return factory(o) })
	}
	// Keep the experiment identity independent of the chosen skin and state
	// path. Restoring a visited session intentionally clears launch-time skin
	// overrides so the user's saved appearance remains authoritative.
	o.experimentID = currentExperiment(o)
	// A session without --state still needs somewhere to keep its layout while
	// visiting another experiment. This private scratch session is removed on
	// exit; it does not opt the user into permanent persistence.
	if o.State == "" {
		directory, err := os.MkdirTemp("", "worldr-experiments-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(directory)
		o.State = filepath.Join(directory, currentExperiment(o)+".json")
		o.Autosave = 0
	}
	root := experimentRoot()
	visited := map[string]Options{}
	var fallback *Options
	var notice string
	for {
		current := currentExperiment(o)
		menu, err := newExperimentMenu(current)
		if err != nil {
			return err
		}
		menu.status = notice
		menu.open = notice != ""
		control := &experimentSession{menu: menu, root: root, visited: visited}
		o.experiments = control
		err = Run(ctx, out, o, func() (experience.Experience, error) { return factory(o) })
		control.close()
		o.experiments = nil
		if err != nil {
			if fallback == nil || ctx.Err() != nil {
				return err
			}
			notice = "Could not open experiment; returned to previous layout."
			fmt.Fprintf(out, "experiment: %v\n", err)
			o, fallback = *fallback, nil
			continue
		}
		if control.next == nil || ctx.Err() != nil {
			return nil
		}
		resume := resumeExperiment(o)
		visited[current] = resume
		fallback = &resume
		o = *control.next
		notice = ""
	}
}

// Persisted content reopens from its manifest, including deliberately empty
// sessions. SDK clients have no content manifest and must be launched again.
func resumeExperiment(o Options) Options {
	o.experimentID = currentExperiment(o)
	o.experiments = nil
	o.Fresh = false
	// Legacy commands are launch requests, not restorable documents. Replaying
	// them would reopen apps the user closed and repeat their startup actions.
	o.Application, o.ApplicationProfile = "", ""
	o.Applications, o.X11Applications, o.ApplicationArgs, o.Launches = nil, nil, nil, nil
	if o.State != "" {
		o.Skin = ""
		o.Project, o.Models, o.Research = "", nil, nil
		o.Terminal, o.Axial = false, false
	}
	return o
}

type experimentResult struct {
	options Options
	err     error
}

type experimentSession struct {
	menu    *experimentMenu
	root    string
	visited map[string]Options
	pending <-chan experimentResult
	cancel  context.CancelFunc
	next    *Options
}

func (s *experimentSession) close() {
	if s.cancel != nil {
		s.cancel()
		// Finish any build/file work before releasing this session.
		if s.pending != nil {
			<-s.pending
		}
	}
	s.menu.Close()
}

func (s *experimentSession) request(ctx context.Context, o Options, id string) {
	if s.pending != nil || id == currentExperiment(o) {
		return
	}
	s.menu.busy, s.menu.status = true, "Preparing experiment..."
	buildContext, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	result := make(chan experimentResult, 1)
	s.pending = result
	previous, known := s.visited[id]
	go func() {
		next, err := prepareExperiment(buildContext, o, id, s.root, previous, known)
		result <- experimentResult{next, err}
	}()
}

func (s *experimentSession) poll(save func() error) bool {
	if s.pending == nil {
		return false
	}
	select {
	case result := <-s.pending:
		s.pending = nil
		s.cancel()
		s.cancel = nil
		s.menu.busy = false
		if result.err == nil {
			result.err = save()
		}
		if result.err != nil {
			s.menu.status = "Could not switch: " + result.err.Error()
			return false
		}
		s.next = &result.options
		return true
	default:
		return false
	}
}

func experimentRoot() string {
	cwd, _ := os.Getwd()
	executable, _ := os.Executable()
	for _, start := range []string{cwd, filepath.Dir(executable)} {
		for dir := start; dir != ""; dir = filepath.Dir(dir) {
			data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			if err == nil && strings.Contains(string(data), "module github.com/codemodify/worldr") {
				return dir
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	return ""
}

func prepareExperiment(ctx context.Context, base Options, id, root string, previous Options, known bool) (Options, error) {
	var selected *experimentDefinition
	for _, def := range experimentCatalog() {
		if def.ID == id {
			copy := def
			selected = &copy
			break
		}
	}
	if selected == nil {
		return Options{}, fmt.Errorf("unknown experiment %q", id)
	}
	if root == "" {
		return Options{}, fmt.Errorf("run from a Worldr checkout to find the experiment files")
	}
	d := *selected
	o := Options{Backend: base.Backend, Card: base.Card, Outputs: append([]uint32(nil), base.Outputs...),
		TakeOver: base.TakeOver, Fullscreen: base.Fullscreen, FPS: base.FPS, GPUMemoryMiB: base.GPUMemoryMiB,
		Autosave: base.Autosave, Experience: d.experience, Skin: d.skin, Width: d.width, Height: d.height,
		State: filepath.Join(root, d.state), AccessibilitySocket: base.AccessibilitySocket}
	if known {
		o = previous
	}
	o.experimentID = d.ID
	if d.client != "" {
		if err := buildExperimentClient(ctx, root, d.client); err != nil {
			return Options{}, err
		}
		if !known {
			o.NativeApplications = []string{filepath.Join(root, "bin", d.client)}
		}
	}
	if err := ctx.Err(); err != nil {
		return Options{}, err
	}
	lock, err := lockSession(o.State)
	if err != nil {
		return Options{}, err
	}
	if lock != nil {
		defer lock.Close()
	}
	if known {
		return o, nil
	}
	if err := seedExperiment(root, d, &o); err != nil {
		return Options{}, err
	}
	return o, nil
}

func buildExperimentClient(ctx context.Context, root, client string) error {
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0755); err != nil {
		return err
	}
	// Use argv, never a shell. The destination is a fixed catalog entry.
	cmd := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(root, "bin", client), "./examples/"+client)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 400 {
			message = message[:400] + "..."
		}
		return fmt.Errorf("build %s: %w %s", client, err, message)
	}
	return nil
}

func seedExperiment(root string, d experimentDefinition, o *Options) error {
	if o.State == "" {
		return nil
	}
	data, err := os.ReadFile(o.State)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	_, recoveryErr := os.Stat(recoveryPath(o.State))
	if recoveryErr != nil && !os.IsNotExist(recoveryErr) {
		return recoveryErr
	}
	if recoveryErr == nil {
		return nil
	}
	first := os.IsNotExist(err)
	if d.template != "" {
		template, err := os.ReadFile(filepath.Join(root, d.template))
		if err != nil {
			return err
		}
		if first {
			// The destination's session lock is held. Use the same atomic write
			// as session saves so a full disk cannot leave a partial seed.
			if err := writeState(o.State, template); err != nil {
				return err
			}
		} else if (d.ID == "navigator" || d.ID == "future-panels") && bytes.Equal(data, template) {
			first = true // Retry a pristine seed after a failed first launch.
		}
	}
	if first && (d.ID == "navigator" || d.ID == "workspace" || d.ID == "future-panels") {
		o.Project, o.Axial = root, true
		if d.ID == "navigator" || d.ID == "future-panels" {
			o.Research = []string{filepath.Join(root, "examples/data/orbit-signals.csv")}
			o.Models = []string{filepath.Join(root, "examples/models/mount.obj")}
		}
		o.Terminal = d.ID == "future-panels"
	}
	return nil
}
