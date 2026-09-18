package app

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/codemodify/worldr/internal/accessibility"
	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/input"
	"github.com/codemodify/worldr/internal/media"
	"github.com/codemodify/worldr/internal/mediaapp"
	"github.com/codemodify/worldr/internal/modelapp"
	"github.com/codemodify/worldr/internal/nativeapps"
	"github.com/codemodify/worldr/internal/noteapp"
	"github.com/codemodify/worldr/internal/photoapp"
	"github.com/codemodify/worldr/internal/platform/linux/host"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/projectapp"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/researchapp"
	"github.com/codemodify/worldr/internal/resourcepath"
	"github.com/codemodify/worldr/internal/sdkhost"
	"github.com/codemodify/worldr/internal/version"
	"github.com/codemodify/worldr/internal/workspace"
)

// Factory separates experience construction from display and storage ownership.
type Factory func() (experience.Experience, error)

// Run owns one experience and its GPU session. Nested and direct Vulkan display
// modes present without a CPU framebuffer. Readback is explicit for export or
// the diagnostic DRM dumb-buffer backend.
func Run(ctx context.Context, out io.Writer, o Options, factory Factory) (runErr error) {
	if o.Version {
		_, err := fmt.Fprintln(out, version.String())
		return err
	}
	if o.ListDevices {
		s, err := native.ListDevices()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, s)
		return err
	}
	if o.ListOutputs {
		return listOutputs(out, o.Card)
	}
	backend := chooseBackend(o)
	if err := validateDirect(o, backend); err != nil {
		return err
	}
	if factory == nil {
		return fmt.Errorf("no experience factory")
	}
	lock, err := lockSession(o.State)
	if err != nil {
		return err
	}
	if lock != nil {
		defer lock.Close()
	}
	work, err := factory()
	if err != nil {
		return fmt.Errorf("create experience: %w", err)
	}
	defer work.Close()
	saved, recovered, recoveryNotice, err := loadRecoverableState(o.State, work, !o.Fresh)
	if err != nil {
		return err
	}
	if o.Fresh {
		saved = nil
	}
	hasLegacyApplications := o.Application != "" || len(o.Applications) > 0 || len(o.X11Applications) > 0 || len(o.Launches) > 0
	_, canHostApplications := work.(experience.ApplicationAware)
	if hasLegacyApplications || len(o.NativeApplications) > 0 || o.Xwayland || o.Terminal || o.Axial || o.Project != "" || len(o.Models) > 0 || len(o.Research) > 0 || saved != nil && (saved.Project != nil || saved.Photo != nil || saved.Media != nil || saved.Axial != nil || len(saved.Terminals) > 0 || len(saved.Models) > 0 || len(saved.Research) > 0 || len(saved.Notes) > 0 || len(saved.Photos) > 0 || len(saved.Videos) > 0) {
		if !canHostApplications {
			return fmt.Errorf("experience %q cannot host application surfaces", work.Info().ID)
		}
	}
	// Validate and anchor an explicit project root before opening a window or
	// starting terminal/legacy processes. Directory contents load off-thread.
	var project *projectapp.Provider
	var savedProject *projectapp.SessionState
	if saved != nil {
		savedProject = saved.Project
	}
	var restoreFailures []error
	if o.Project != "" || savedProject != nil {
		if checker, ok := work.(experience.ApplicationPlacementChecker); ok {
			err = checker.CheckApplicationPlacement("native:project-browser")
		}
		if err == nil {
			project, err = openSessionProject(o.Project, savedProject)
		}
		if err != nil {
			if o.Project != "" {
				return fmt.Errorf("native project browser: %w", err)
			}
			restoreFailures = append(restoreFailures, fmt.Errorf("Files %q: %w", savedProject.Root, err))
			fmt.Fprintf(out, "session restore: %v\n", restoreFailures[len(restoreFailures)-1])
		}
	}
	p, err := openPresenter(o, backend, work.Info().Title, work.Atlas())
	if err != nil {
		if project != nil {
			_ = project.Close()
		}
		return err
	}
	var applications *applicationController
	// If GPU retirement itself failed, release remaining exported handles only
	// after the renderer is destroyed. Normal per-frame retirement empties this.
	defer func() {
		if applications != nil {
			for id, texture := range applications.retiredBacking {
				runErr = errors.Join(runErr, texture.Close())
				delete(applications.retiredBacking, id)
			}
		}
	}()
	defer p.close()
	var providers []experience.Applications
	var content *applicationHub
	defer func() {
		// A partial startup owns the same providers as a running session. Keep
		// teardown in reverse registration order, before closing the presenter.
		owner := content
		if owner == nil {
			owner = newApplicationHub(providers...)
		}
		if err := owner.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close applications: %w", err))
		}
		// Close returns final producer snapshots. Retire device imports before
		// releasing their exported backing, including partial-startup failures.
		for _, id := range owner.RetiredTextures() {
			err := p.releaseTexture(id)
			runErr = errors.Join(runErr, err)
			if err == nil && applications != nil {
				runErr = errors.Join(runErr, applications.releaseTextureBacking(id))
			}
		}
	}()
	var files *filesProvider
	if canHostApplications {
		root := o.Project
		if root == "" && savedProject != nil {
			root = savedProject.Root
		}
		if root == "" {
			root, _ = os.Getwd()
		}
		files = &filesProvider{current: project, root: root, next: 1}
		providers = append(providers, files)
	}
	if project != nil {
		if state, ok := project.SessionState(); ok {
			fmt.Fprintln(out, "application: worldr native project browser ·", state.Root)
		}
	}
	if canHostApplications {
		if hasLegacyApplications {
			applications, err = launchApplication(o, out, configureApplicationRenderer(p))
		} else {
			applications, err = openApplicationController(out, configureApplicationRenderer(p))
			if err == nil && o.Xwayland {
				if err = applications.enableXwayland(); err != nil {
					applications.close()
				}
			}
		}
		if err != nil {
			return err
		}
		providers = append(providers, applications)
	}
	var terminal *nativeapps.Manager
	if canHostApplications {
		terminal = nativeapps.NewManager(nativeapps.Options{Env: applications.terminalEnvironment(os.Environ())})
		providers = append(providers, terminal)
	}
	var player *mediaapp.Collection
	var photos *photoapp.Collection
	var models *modelapp.Manager
	var research *researchapp.Manager
	var notes *noteapp.Manager
	var axial *workspace.AxialApplicationManager
	if canHostApplications {
		player = mediaapp.NewCollection(media.Options{})
		providers = append(providers, player)
		photos = photoapp.NewCollection()
		providers = append(providers, photos)
		models = modelapp.NewManager()
		providers = append(providers, models)
		research = researchapp.NewManager()
		providers = append(providers, research)
		notes = noteapp.NewManager()
		providers = append(providers, notes)
		axial, err = workspace.NewAxialApplicationManager()
		if err != nil {
			return err
		}
		providers = append(providers, axial)
	}
	nativeAppInstances := make(map[string]int)
	for _, command := range o.NativeApplications {
		provider, err := sdkhost.Open(ctx, command, nil, out)
		if err != nil {
			return fmt.Errorf("native SDK application: %w", err)
		}
		nativeAppInstances[provider.ApplicationID()]++
		instance := nativeAppInstances[provider.ApplicationID()]
		if err := provider.SetInstanceOrdinal(instance); err != nil {
			_ = provider.Close()
			return fmt.Errorf("native SDK application: %w", err)
		}
		providers = append(providers, provider)
		fmt.Fprintf(out, "application: %s · Worldr native-app SDK v1 · instance %d\n", command, instance)
	}
	if len(providers) > 0 {
		content = newApplicationHub(providers...)
	}
	session := &workspaceSession{work: work, project: project, terminals: terminal, models: models, research: research, notes: notes, axial: axial}
	if photos != nil {
		session.photos = photos
	}
	if player != nil {
		session.media = player
	}
	if project == nil && savedProject != nil {
		session.pending.Project = savedProject
	}
	restoreFailures = append(restoreFailures, session.restore(saved, content, out)...)
	if o.Axial && len(axial.SessionStates()) == 0 {
		if err := checkSessionPlacement(work, content, workspace.AxialApplicationKey); err != nil {
			return err
		}
		if _, err := axial.LaunchApplication("axial"); err != nil {
			return fmt.Errorf("hosted AXIAL: %w", err)
		}
		fmt.Fprintln(out, "application: AXIAL / 07 · hosted native 3D study")
	}
	for _, path := range o.Models {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		already := false
		for _, state := range models.SessionStates() {
			already = already || state.Source == absolute || state.Document == absolute
		}
		if already {
			continue
		}
		key, err := models.NextKey()
		if err == nil {
			err = checkSessionPlacement(work, content, key)
		}
		if err != nil {
			return err
		}
		absolute, err = filepath.EvalSymlinks(absolute)
		if err != nil {
			return fmt.Errorf("model: %w", err)
		}
		file, err := resourcepath.OpenFile(absolute)
		if err != nil {
			return fmt.Errorf("model: %w", err)
		}
		_, err = models.OpenFile(file, filepath.Base(path))
		if err != nil {
			file.Close()
			return fmt.Errorf("model: %w", err)
		}
	}
	for _, path := range o.Research {
		if !researchapp.IsDatasetPath(path) {
			return fmt.Errorf("research: unsupported dataset name %q", path)
		}
		absolute, err := filepath.Abs(path)
		if err == nil {
			absolute, err = filepath.EvalSymlinks(absolute)
		}
		if err != nil {
			return fmt.Errorf("research: %w", err)
		}
		already := false
		for _, state := range research.SessionStates() {
			already = already || state.Source == absolute
		}
		if already {
			continue
		}
		key, err := research.NextKey()
		if err == nil {
			err = checkSessionPlacement(work, content, key)
		}
		if err != nil {
			return err
		}
		file, err := resourcepath.OpenFile(absolute)
		if err != nil {
			return fmt.Errorf("research: %w", err)
		}
		if _, err = research.OpenFile(file, filepath.Base(path)); err != nil {
			file.Close()
			return fmt.Errorf("research: %w", err)
		}
	}
	if o.Terminal && len(terminal.Surfaces()) == 0 {
		key, err := terminal.NextTerminalKey()
		if err == nil {
			err = checkSessionPlacement(work, content, key)
		}
		if err == nil {
			_, err = terminal.LaunchApplication("terminal")
		}
		if err != nil {
			return fmt.Errorf("native terminal: %w", err)
		}
		fmt.Fprintln(out, "application: worldr native terminal · PTY shell")
	}
	if content != nil {
		work.(experience.ApplicationAware).SetApplications(content)
		defer work.(experience.ApplicationAware).SetApplications(nil)
	}
	var semanticBridge *accessibility.Server
	if o.AccessibilitySocket != "" {
		semanticBridge, err = accessibility.Listen(o.AccessibilitySocket)
		if err != nil {
			return err
		}
		defer func() { runErr = errors.Join(runErr, semanticBridge.Close()) }()
		if content != nil {
			if err := semanticBridge.Publish(content.AccessibilitySnapshot()); err != nil {
				return err
			}
		}
	}
	if project != nil {
		connectFileBrowserWithNativeTools(project, player, photos, research, notes, content, work, models)
		connectTerminalBrowser(project, terminal, content, work)
	}
	if files != nil {
		files.check = func() error { return checkSessionPlacement(work, content, "native:project-browser") }
		files.onOpen = func(p *projectapp.Provider) {
			project = p
			session.project = p
			connectFileBrowserWithNativeTools(p, player, photos, research, notes, content, work, models)
			connectTerminalBrowser(p, terminal, content, work)
		}
	}
	prepareInputLayout(work, p.w, p.h)
	if recovered {
		fmt.Fprintln(out, "session: recovered from", recoveryPath(o.State))
	}
	if recoveryNotice != "" {
		fmt.Fprintln(out, "session:", recoveryNotice)
		notifyWorkspace(work, recoveryNotice)
	}
	if len(restoreFailures) > 0 {
		notifyWorkspace(work, fmt.Sprintf("Could not reopen %d item(s): %v", len(restoreFailures), restoreFailures[0]))
	} else if saved != nil && content != nil {
		fmt.Fprintf(out, "session: reopened %d native windows; terminal folders use fresh shells\n", len(content.Surfaces()))
	}
	var checkpoints *autosaver
	if _, ok := work.(experience.Checkpointer); ok {
		checkpoints = newAutosaver(o.State, o.Autosave)
	}
	defer func() {
		if err := checkpoints.Close(); err != nil {
			fmt.Fprintf(out, "autosave: %v\n", err)
		}
	}()
	var clipboard *clipboardBroker
	var clipboardEndpoints []clipboardEndpoint
	if content != nil && p.win != nil {
		clipboardEndpoints = append(clipboardEndpoints, hostClipboard{p.win})
	}
	if applications != nil {
		if server, ok := applications.server.(applicationClipboardServer); ok {
			clipboardEndpoints = append(clipboardEndpoints, applicationClipboard{server})
		}
		if bridge, ok := applications.x11.(x11ClipboardBridge); ok {
			clipboardEndpoints = append(clipboardEndpoints, x11Clipboard{bridge})
		}
	}
	if terminal != nil {
		endpoint := newNativeClipboard(terminal)
		defer endpoint.close()
		clipboardEndpoints = append(clipboardEndpoints, endpoint)
	}
	if files != nil {
		endpoint := newNativeClipboard(files)
		defer endpoint.close()
		clipboardEndpoints = append(clipboardEndpoints, endpoint)
	}
	if models != nil {
		endpoint := newNativeClipboard(models)
		defer endpoint.close()
		clipboardEndpoints = append(clipboardEndpoints, endpoint)
	}
	if notes != nil {
		endpoint := newNativeClipboard(notes)
		defer endpoint.close()
		clipboardEndpoints = append(clipboardEndpoints, endpoint)
	}
	if client, ok := work.(nativeClipboardClient); ok {
		endpoint := newNativeClipboard(client)
		defer endpoint.close()
		clipboardEndpoints = append(clipboardEndpoints, endpoint)
	}
	if len(clipboardEndpoints) > 0 {
		clipboard = newClipboardBroker(clipboardEndpoints...)
	}
	fmt.Fprintf(out, "worldr %s — %s\nbackend: %s\ndevice: %s\nscene: %dx%d\n", version.String(), work.Info().Title, backend, p.vk.DeviceName(), p.w, p.h)
	if backend == "nested" {
		fmt.Fprintln(out, "transport: Vulkan swapchain on host surface; no frame readback")
	}
	if backend == "drm" {
		fmt.Fprintln(out, "transport: diagnostic GPU readback to DRM dumb buffer")
	}
	if semanticBridge != nil {
		fmt.Fprintln(out, "accessibility: native semantic stream ·", o.AccessibilitySocket)
	}
	quitChord := "Ctrl+Q"
	if content != nil {
		quitChord = "Ctrl+Alt+Q"
	}
	fmt.Fprintf(out, "controls: %s · %s quit\n", work.Info().Controls, quitChord)
	if o.State != "" {
		fmt.Fprintf(out, "document: %s · Ctrl+S from workspace saves; saved on normal exit\n", o.State)
		if o.Autosave > 0 && checkpoints != nil {
			fmt.Fprintf(out, "recovery: checkpoint every %s\n", o.Autosave)
		}
	}
	if backend == "headless" && o.Frames == 0 && o.Duration == 0 {
		o.Frames = 1
	}
	period := time.Second / time.Duration(o.FPS)
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	start, last := time.Now(), time.Now()
	metrics := runMetrics{start: start}
	var recovery graphicsRecovery
	if o.Metrics != "" {
		defer func() {
			if metrics.end.IsZero() {
				metrics.sampleMemory(p)
			}
			if err := metrics.write(o.Metrics, backend, p.w, p.h, runErr); err != nil {
				runErr = errors.Join(runErr, err)
			} else {
				fmt.Fprintf(out, "metrics: %s\n", o.Metrics)
			}
		}()
	}
	// Fatal platform faults still checkpoint committed user work. This does not
	// replay processes or turn an unfinished pointer gesture into a saved edit.
	defer func() {
		if runErr != nil && o.State != "" {
			_ = checkpoints.Flush()
			if data, err := encodeState(work, session.snapshot(), true); err == nil {
				if err = writeState(recoveryPath(o.State), data); err != nil {
					fmt.Fprintf(out, "recovery checkpoint failed: %v\n", err)
				}
			}
		}
	}()
	var keys keyState
	var events []host.Event
	var directEvents []experience.Event
	frames := 0
	var renderTime, maxRender time.Duration
	cursorX, cursorY := float32(p.w/2), float32(p.h/2)
	cursorVisible := p.pointer != nil
	var cursor cursorOverlay
	handle := func(e experience.Event) (bool, error) {
		if e.Kind == experience.KeyInput || e.Kind == experience.PointerDown || e.Kind == experience.PointerMove || e.Kind == experience.PointerScroll || e.Kind == experience.TextCommit {
			metrics.dispatch(time.Now())
		}
		if content != nil {
			content.seat(e)
		}
		quit, err := dispatchEvent(work, e, o.State, out, func() error { return session.save(o.State, checkpoints) })
		session.reconcile()
		if err == nil && p.win != nil {
			err = p.win.SetTextInput(textInputState(work))
		}
		return quit, err
	}
loop:
	for {
		frameBegin := time.Now()
		select {
		case <-ctx.Done():
			break loop
		default:
		}
		changed, seatErr := p.pollSeat(time.Now())
		if seatErr != nil {
			return fmt.Errorf("direct session: %w", seatErr)
		}
		if changed {
			keys = keyState{}
			for _, e := range []experience.Event{{Kind: experience.KeyboardCancel}, {Kind: experience.PointerCancel}} {
				if _, err := handle(e); err != nil {
					return err
				}
			}
			prepareInputLayout(work, p.w, p.h)
			if p.pointer != nil {
				cursorX, cursorY = float32(p.pointer.X), float32(p.pointer.Y)
				cursorVisible = true
			}
		}
		if notice := p.takeDirectNotice(); notice != "" {
			fmt.Fprintln(out, "session:", notice)
			notifyWorkspace(work, notice)
		}
		if p.direct != nil && p.vk == nil {
			if err := checkpoints.Tick(time.Now(), func() ([]byte, error) { return encodeState(work, session.snapshot(), true) }); err != nil {
				fmt.Fprintf(out, "autosave: %v\n", err)
			}
			if o.Duration > 0 && time.Since(start) >= o.Duration {
				break loop
			}
			select {
			case <-ctx.Done():
				break loop
			case <-ticker.C:
			}
			last = time.Now()
			continue
		}
		if content != nil {
			if err := content.Poll(); err != nil {
				return err
			}
		}
		session.reconcile()
		if p.win != nil {
			events, err = p.win.Poll(events[:0])
			if err != nil {
				return err
			}
			if applications != nil {
				if err := applications.updateOutputScale(p); err != nil {
					return err
				}
			}
			w, h := p.win.Size()
			resized := w != p.w || h != p.h
			if resized {
				if err := p.resize(w, h); err != nil && !errors.Is(err, native.ErrNotReady) {
					if !recoverableGraphics(err) {
						return err
					}
					if err = p.recover(&recovery, time.Now(), err); err != nil {
						return err
					}
					metrics.recoveries++
				}
				metrics.resizes++
				prepareInputLayout(work, p.w, p.h)
			}
			for _, event := range events {
				if event.Kind == host.Close {
					break loop
				}
				e := hostEvent(event)
				// A batch may contain events from both sides of a host configure.
				// Discard ambiguous button edges after canceling the old capture.
				if resized && isPointerTransition(e) {
					continue
				}
				if e.Kind == experience.PointerMove || e.Kind == experience.PointerDown || e.Kind == experience.PointerUp {
					cursorX, cursorY = e.X, e.Y
					cursorVisible = true
				}
				if e.Kind == experience.PointerCancel {
					cursorVisible = false
				}
				quit, err := handle(e)
				if err != nil {
					return err
				}
				if quit {
					break loop
				}
			}
		} else if p.pointer != nil {
			p.pointer.Poll()
			if err := p.pointer.Err(); err != nil {
				return fmt.Errorf("direct input: %w", err)
			}
			directEvents = keys.directEvents(directEvents[:0], p.pointer.Events)
			for _, e := range directEvents {
				if vt := directVT(e); vt != 0 && p.direct != nil {
					if err := p.direct.session.SwitchSession(vt); err != nil {
						notifyWorkspace(work, err.Error())
					}
					break
				}
				if e.Kind == experience.PointerMove || e.Kind == experience.PointerDown || e.Kind == experience.PointerUp || e.Kind == experience.PointerScroll {
					cursorX, cursorY = e.X, e.Y
					cursorVisible = true
				}
				if e.Kind == experience.PointerCancel {
					cursorVisible = false
				}
				quit, err := handle(e)
				if err != nil {
					return err
				}
				if quit {
					break loop
				}
			}
		}
		if clipboard != nil {
			clipboard.sync()
		}
		now := time.Now()
		dt := now.Sub(last)
		last = now
		if dt > max(period, 100*time.Millisecond) {
			dt = max(period, 100*time.Millisecond)
		}
		if o.Demo {
			dt = period
			if demo, ok := work.(experience.Demonstrator); ok {
				demo.Demo(time.Duration(frames) * period)
			}
		}
		work.Update(dt)
		if semanticBridge != nil && content != nil {
			if err := semanticBridge.Publish(content.AccessibilitySnapshot()); err != nil {
				return err
			}
		}
		if err := checkpoints.Tick(now, func() ([]byte, error) { return encodeState(work, session.snapshot(), true) }); err != nil {
			fmt.Fprintf(out, "autosave: %v\n", err)
			notifyWorkspace(work, "Autosave failed: "+err.Error())
		}
		frame := work.Draw(p.w, p.h)
		if p.win != nil {
			if err := p.win.SetTextInput(textInputState(work)); err != nil {
				return err
			}
		}
		if owner, ok := work.(experience.ApplicationTextureRetirer); ok {
			for _, id := range owner.RetiredTextures() {
				if err := p.releaseTexture(id); err != nil {
					return err
				}
			}
		}
		if content != nil {
			for _, id := range content.RetiredGeometryIDs() {
				if err := p.releaseGeometry(id); err != nil {
					return err
				}
			}
			for _, id := range content.RetiredTextures() {
				if err := p.releaseTexture(id); err != nil {
					return err
				}
				if applications != nil {
					if err := applications.releaseTextureBacking(id); err != nil {
						return err
					}
				}
			}
		}
		if applications != nil && applications.err != nil {
			return fmt.Errorf("application input: %w", applications.err)
		}
		if cursorVisible {
			frame = cursor.appendFor(frame, cursorX, cursorY, work.Atlas(), work)
		}
		before := time.Now()
		err = p.renderFrame(frame, [4]float32{.025, .034, .043, 1})
		if p.direct != nil && (errors.Is(err, native.ErrSurfaceLost) || errors.Is(err, native.ErrOutOfDate)) {
			if err = p.invalidateDirect(err); err != nil {
				return err
			}
			keys = keyState{}
			handle(experience.Event{Kind: experience.KeyboardCancel})
			handle(experience.Event{Kind: experience.PointerCancel})
			continue
		}
		if recoverableGraphics(err) {
			notifyWorkspace(work, "Graphics device interrupted; restoring the workspace renderer.")
			if err = p.recover(&recovery, time.Now(), err); err != nil {
				return err
			}
			metrics.recoveries++
			prepareInputLayout(work, p.w, p.h)
			select {
			case <-ctx.Done():
				break loop
			case <-ticker.C:
			}
			continue
		}
		if errors.Is(err, native.ErrOutOfDate) || errors.Is(err, native.ErrNotReady) {
			if errors.Is(err, native.ErrOutOfDate) {
				w, h := p.w, p.h
				if p.win != nil {
					w, h = p.win.Size()
				}
				if err := p.resize(w, h); err != nil {
					return err
				}
				prepareInputLayout(work, p.w, p.h)
			}
			if o.Duration > 0 && time.Since(start) >= o.Duration {
				break loop
			}
			select {
			case <-ctx.Done():
				break loop
			case <-ticker.C:
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("render frame %d: %w", frames, err)
		}
		endRender := time.Now()
		metrics.frame(frameBegin, before, endRender, p)
		elapsed := endRender.Sub(before)
		renderTime += elapsed
		if elapsed > maxRender {
			maxRender = elapsed
		}
		if p.drm != nil && !p.drm.PlanesOnly() {
			if err := p.drm.PresentBGRA(p.pixels, uint32(p.w*4)); err != nil {
				return err
			}
		}
		frames++
		if (o.Frames > 0 && frames >= o.Frames) || (o.Duration > 0 && time.Since(start) >= o.Duration) {
			break
		}
		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
		}
	}
	metrics.end = time.Now()
	metrics.sampleMemory(p)
	// An interrupted pointer capture is not a committed document edit.
	work.Handle(experience.Event{Kind: experience.PointerCancel})
	if err := session.save(o.State, checkpoints); err != nil {
		return err
	}
	if o.Snapshot != "" {
		// Export re-renders the same document into an offscreen image. The interactive
		// presenter never needs a readback allocation for this optional operation.
		capture, err := native.OpenVK(false, uint32(p.w), uint32(p.h))
		if err != nil {
			return err
		}
		defer capture.Close()
		if err := capture.SetSceneAtlas(work.Atlas()); err != nil {
			return err
		}
		pixels := make([]byte, p.w*p.h*4)
		if err := capture.RenderFrame(work.Draw(p.w, p.h), [4]float32{.025, .034, .043, 1}, pixels); err != nil {
			return err
		}
		if err := savePNG(o.Snapshot, pixels, p.w, p.h); err != nil {
			return err
		}
		fmt.Fprintf(out, "snapshot: %s\n", o.Snapshot)
	}
	if frames > 0 {
		fmt.Fprintf(out, "rendered: %d frames · %dx%d · %dx sampling · mean submit/wait %.2f ms · max %.2f ms\n", frames, p.w, p.h, p.samples, float64(renderTime)/float64(frames)/1e6, float64(maxRender)/1e6)
	}
	return nil
}

type presenter struct {
	direct  *directPresentation
	outputs *native.OutputSet
	samples int
	vk      *native.VK
	win     *host.Window
	drm     *native.DRM
	pointer *input.Pointer
	w, h    int
	pixels  []byte
}

func openPresenter(o Options, backend, title string, atlas render.Atlas) (*presenter, error) {
	p := &presenter{w: o.Width, h: o.Height}
	var err error
	if backend == "vk-display" || backend == "drm" {
		if err := p.startDirect(o, backend, atlas); err != nil {
			p.close()
			return nil, err
		}
		return p, nil
	}
	if backend == "nested" {
		p.win, err = host.Open(title, p.w, p.h, o.Fullscreen)
		if err != nil {
			return nil, err
		}
		p.w, p.h = p.win.Size()
	}
	if backend == "nested" {
		display, surface := p.win.Handles()
		p.vk, err = native.OpenVKWayland(display, surface, uint32(p.w), uint32(p.h))
	} else {
		p.vk, err = native.OpenVK(false, uint32(p.w), uint32(p.h))
	}
	if err != nil {
		p.close()
		return nil, fmt.Errorf("%s Vulkan: %w", backend, err)
	}
	if err := p.vk.SetMemoryBudget(o.GPUMemoryMiB << 20); err != nil {
		p.close()
		return nil, err
	}
	w, h := p.vk.Size()
	p.w, p.h = int(w), int(h)
	if !validExtent(p.w, p.h) {
		p.close()
		return nil, fmt.Errorf("unsupported render extent %dx%d", p.w, p.h)
	}
	if err := p.vk.SetSceneAtlas(atlas); err != nil {
		p.close()
		return nil, err
	}
	p.samples = p.vk.SampleCount()
	return p, nil
}
func validExtent(w, h int) bool { return w > 0 && h > 0 && w <= 7680 && h <= 4320 }
func (p *presenter) resize(w, h int) error {
	if w == 0 || h == 0 {
		return native.ErrNotReady
	}
	if !validExtent(w, h) {
		return fmt.Errorf("unsupported host size %dx%d", w, h)
	}
	if err := p.vk.Resize(uint32(w), uint32(h)); err != nil {
		return err
	}
	width, height := p.vk.Size()
	p.w, p.h = int(width), int(height)
	return nil
}
func (p *presenter) close() {
	if p.direct != nil {
		_ = p.closeDirect()
		_ = p.direct.session.Close()
		p.direct = nil
		return
	}
	if p.pointer != nil {
		p.pointer.Close()
	}
	if p.vk != nil {
		p.vk.Close()
	}
	if p.drm != nil {
		p.drm.Close()
	}
	if p.win != nil {
		p.win.Close()
	}
}

func savePNG(path string, bgra []byte, w, h int) error {
	if w < 1 || h < 1 || len(bgra) != w*h*4 {
		return fmt.Errorf("invalid snapshot buffer")
	}
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(bgra); i += 4 {
		im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = bgra[i+2], bgra[i+1], bgra[i], 255
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	encErr := png.Encode(f, im)
	closeErr := f.Close()
	if encErr != nil {
		return encErr
	}
	return closeErr
}
