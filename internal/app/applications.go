package app

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/apps"
	"github.com/codemodify/worldr/internal/platform/linux/xwayland"
	"github.com/codemodify/worldr/internal/render"
)

type applicationServer interface {
	Poll() ([]apps.Surface, error)
	Focus(uint64) error
	Pointer(uint64, float32, float32) error
	Button(uint32, bool, uint32) error
	Axis(float32, float32, uint32) error
	Key(uint32, bool, uint32, uint32, uint32, uint32, uint32) error
	Modifiers(uint32, uint32, uint32, uint32) error
	SetKeymap(string) error
	SetRepeat(int32, int32) error
	Resize(uint64, int, int) error
	CloseSurface(uint64) error
	RequestClose() error
	Close() error
}

type applicationImage struct {
	surface            experience.ApplicationSurface
	revision           uint64
	logicalW, logicalH int
}

type applicationX11Bridge interface {
	Poll() error
	Close() error
	Environment([]string) []string
	Window(uint64) (xwayland.Window, bool)
	Focus(uint64) error
	Resize(uint64, int, int) error
	CloseSurface(uint64) error
	DragActive(uint64) bool
	DragMotion(uint64, uint64, float32, float32, uint32) error
	DropXDND(uint64, uint64, uint32) error
	CancelXDND() error
}

type applicationProcess struct {
	cmd        *exec.Cmd
	wait       chan error
	exited     bool
	reaped     bool
	observeErr error
	exitErr    error
	slot       string
}

// applicationController translates protocol content into renderer resources.
// It belongs to the same goroutine as the experience and renderer. Protocol IDs
// never escape to document storage; only the scene decides focus and placement.
type applicationController struct {
	server                                          applicationServer
	x11                                             applicationX11Bridge
	socket                                          string
	images                                          map[uint64]*applicationImage
	surfaces                                        []experience.ApplicationSurface
	retired                                         []uint64
	retiredBacking                                  map[uint64]*render.Texture
	err                                             error
	children                                        []*applicationProcess
	exited                                          bool
	closed                                          bool
	closeErr                                        error
	log                                             applicationLog
	output                                          io.Writer
	cursorTexture                                   *render.Texture
	cursorOwner, cursorImageID, cursorImageRevision uint64
}

// An empty controller accepts GUI clients started later by a native terminal.
// Process launch and ownership remain explicit; opening it starts no programs.
func openApplicationController(output io.Writer, configure ...func(*applicationController) error) (*applicationController, error) {
	server, err := apps.Open(900, 560)
	if err != nil {
		return nil, fmt.Errorf("application host: %w", err)
	}
	a := &applicationController{server: server, socket: server.Socket(), images: make(map[uint64]*applicationImage), output: output}
	for _, configure := range configure {
		if configure != nil {
			if err := configure(a); err != nil {
				a.close()
				return nil, err
			}
		}
	}
	return a, nil
}

func launchApplication(o Options, output io.Writer, configure ...func(*applicationController) error) (*applicationController, error) {
	launches, err := applicationLaunches(o)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(launches))
	for i, launch := range launches {
		path, err := exec.LookPath(launch.Command)
		if err != nil {
			return nil, fmt.Errorf("find application %q: %w", launch.Command, err)
		}
		paths[i] = path
	}
	a, err := openApplicationController(output, configure...)
	if err != nil {
		return nil, err
	}
	wantX11 := o.Xwayland
	for _, launch := range launches {
		wantX11 = wantX11 || launch.X11
	}
	if wantX11 {
		if err := a.enableXwayland(); err != nil {
			a.close()
			return nil, err
		}
	}
	for i, path := range paths {
		launch := launches[i]
		child := &applicationProcess{cmd: exec.Command(path, launch.Args...), wait: make(chan error, 1), slot: launch.ID}
		child.cmd.Env = applicationEnvironment(os.Environ(), a.socket)
		protocol := "Wayland"
		if launch.X11 {
			child.cmd.Env = a.x11.Environment(os.Environ())
			protocol = "X11"
		}
		child.cmd.Stdout, child.cmd.Stderr = &a.log, &a.log
		prepareApplicationProcess(child.cmd)
		if err := child.cmd.Start(); err != nil {
			a.close()
			return nil, fmt.Errorf("start application %q: %w", launch.Command, err)
		}
		a.children = append(a.children, child)
		go func() { child.wait <- observeApplicationProcess(child.cmd) }()
		fmt.Fprintf(output, "application: %s (%s) · private %s host\n", launch.Command, child.slot, protocol)
	}
	return a, nil
}

func (a *applicationController) collectExits(report bool) {
	a.exited = len(a.children) > 0
	for _, child := range a.children {
		if !child.exited {
			select {
			case err := <-child.wait:
				child.exited = true
				child.observeErr = err
				child.reaped = !applicationExitPinned()
				if report {
					if err == nil {
						fmt.Fprintf(a.output, "application %s launcher exited; workspace remains open\n", child.slot)
					} else {
						fmt.Fprintf(a.output, "application %s exited: %v; workspace remains open\n", child.slot, err)
					}
				}
			default:
			}
		}
		a.exited = a.exited && child.exited
	}
}

// Launch positions give independent clients stable associations even when the
// compositor receives their first buffers in a different order. Reconnecting a
// surface reuses an available window slot; live sibling keys never move.
func (a *applicationController) surfaceKey(surface apps.Surface, used map[string]bool) string {
	base := ""
	pid := surface.PID
	if a.x11 != nil {
		if window, ok := a.x11.Window(surface.ID); ok {
			pid = window.PID
		}
	}
	for _, child := range a.children {
		if pid != 0 && !child.reaped && child.observeErr == nil && applicationProcessMatches(child.cmd, pid) {
			base = child.slot
			break
		}
	}
	if base == "" {
		identity := sha256.Sum256([]byte(surface.AppID))
		base = fmt.Sprintf("external-%x", identity[:8])
	}
	for ordinal := 1; ; ordinal++ {
		key := fmt.Sprintf("%s/window-%d", base, ordinal)
		if !used[key] {
			used[key] = true
			return key
		}
	}
}

// The adapter changes only the child environment. Removing inherited display
// sockets prevents a launched client from accidentally attaching to the desktop
// that hosts worldr itself.
func applicationEnvironment(parent []string, socket string) []string {
	child := make([]string, 0, len(parent)+2)
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "WAYLAND_DISPLAY", "WAYLAND_SOCKET", "DISPLAY", "XAUTHORITY", "XDG_SESSION_TYPE":
			continue
		}
		child = append(child, entry)
	}
	return append(child, "WAYLAND_DISPLAY="+socket, "XDG_SESSION_TYPE=wayland")
}

func (a *applicationController) enableXwayland() error {
	if a.closed {
		return apps.ErrClosed
	}
	if a.x11 != nil {
		return nil
	}
	server, ok := a.server.(*apps.Server)
	if !ok {
		return fmt.Errorf("Xwayland requires the native application server")
	}
	bridge, err := xwayland.Open(server, a.output)
	if err != nil {
		return err
	}
	a.x11 = bridge
	return nil
}

// Native shells can launch either protocol when Xwayland was explicitly
// enabled. Both sockets and Xauthority belong to this workspace instance.
func (a *applicationController) terminalEnvironment(parent []string) []string {
	if a.x11 == nil {
		return applicationEnvironment(parent, a.socket)
	}
	env := a.x11.Environment(parent)
	for i, entry := range env {
		if strings.HasPrefix(entry, "XDG_SESSION_TYPE=") {
			env[i] = "XDG_SESSION_TYPE=wayland"
		}
	}
	return env
}

func (a *applicationController) poll() error {
	if a.closed {
		return apps.ErrClosed
	}
	if a.err != nil {
		return a.err
	}
	a.collectExits(true)
	if log := a.log.drain(); log != "" {
		fmt.Fprint(a.output, log)
	}
	if a.x11 != nil {
		if err := a.x11.Poll(); err != nil {
			return fmt.Errorf("poll Xwayland: %w", err)
		}
	}
	current, err := a.server.Poll()
	a.collectServerTextures()
	if err != nil {
		return fmt.Errorf("poll applications: %w", err)
	}
	a.surfaces = a.surfaces[:0]
	seen := make(map[uint64]bool, len(current))
	usedKeys := make(map[string]bool, len(current))
	for _, s := range current {
		seen[s.ID] = true
		if entry := a.images[s.ID]; entry != nil {
			usedKeys[entry.surface.Key] = true
		}
	}
	for _, s := range current {
		entry := a.images[s.ID]
		if entry == nil {
			entry = &applicationImage{surface: experience.ApplicationSurface{ID: s.ID, Key: a.surfaceKey(s, usedKeys)}}
			a.images[s.ID] = entry
		}
		if err := a.updateApplicationImage(entry, s); err != nil {
			return fmt.Errorf("application image: %w", err)
		}
		entry.surface.Title, entry.surface.AppID = s.Title, s.AppID
		entry.logicalW, entry.logicalH = s.LogicalWidth, s.LogicalHeight
		if entry.logicalW <= 0 || entry.logicalH <= 0 {
			entry.logicalW, entry.logicalH = s.Width, s.Height
		}
		a.surfaces = append(a.surfaces, entry.surface)
	}
	for id, entry := range a.images {
		if !seen[id] {
			a.retireApplicationImage(entry)
			delete(a.images, id)
		}
	}
	if a.cursorTexture != nil && !seen[a.cursorOwner] {
		a.retireCursor()
	}
	return nil
}

// Query lazily so pointer cancellation or a focus transfer earlier in this
// frame cannot leave a stale client cursor. One retained texture is sufficient
// for the seat; hotspot-only updates do not require uploading its image again.
func (a *applicationController) ApplicationCursor(id uint64) (experience.ApplicationCursor, bool) {
	server, ok := a.server.(interface{ Cursor() apps.Cursor })
	if !ok || a.closed || id == 0 || a.images[id] == nil {
		return experience.ApplicationCursor{}, false
	}
	cursor := server.Cursor()
	if !cursor.Set || cursor.SurfaceID != id {
		return experience.ApplicationCursor{}, false
	}
	if cursor.Hidden {
		return experience.ApplicationCursor{Hidden: true}, true
	}
	limit := 512
	if cursor.DragIcon {
		limit = 4096
	}
	logical := [2]float32{}
	if cursor.LogicalWidth > 0 && cursor.LogicalHeight > 0 {
		logical = [2]float32{float32(cursor.LogicalWidth), float32(cursor.LogicalHeight)}
	}
	if cursor.Width < 1 || cursor.Height < 1 || cursor.Width > limit || cursor.Height > limit || cursor.Scale < 1 || cursor.Scale > 8 || logical == ([2]float32{}) && (cursor.Width%cursor.Scale != 0 || cursor.Height%cursor.Scale != 0) {
		a.remember(fmt.Errorf("invalid application cursor dimensions or scale"))
		return experience.ApplicationCursor{}, false
	}
	if a.cursorTexture == nil || a.cursorImageID != cursor.ImageID || a.cursorImageRevision != cursor.ImageRevision {
		var err error
		if a.cursorTexture == nil {
			a.cursorTexture, err = render.NewTexture(cursor.Width, cursor.Height, cursor.Pixels)
		} else {
			err = a.cursorTexture.Replace(cursor.Width, cursor.Height, cursor.Pixels)
		}
		if err != nil {
			a.remember(fmt.Errorf("application cursor image: %w", err))
			return experience.ApplicationCursor{}, false
		}
		a.cursorImageID, a.cursorImageRevision = cursor.ImageID, cursor.ImageRevision
	}
	a.cursorOwner = cursor.SurfaceID
	return experience.ApplicationCursor{Texture: a.cursorTexture, HotspotX: float32(cursor.HotspotX), HotspotY: float32(cursor.HotspotY), Scale: float32(cursor.Scale), LogicalSize: logical}, true
}

func (a *applicationController) retireCursor() {
	if a.cursorTexture != nil {
		a.retired = append(a.retired, a.cursorTexture.ID())
	}
	a.cursorTexture = nil
	a.cursorOwner, a.cursorImageID, a.cursorImageRevision = 0, 0, 0
}

func (a *applicationController) Surfaces() []experience.ApplicationSurface { return a.surfaces }
func (a *applicationController) remember(err error) {
	if a.err == nil && err != nil {
		a.err = err
	}
}
func (a *applicationController) Focus(id uint64) {
	if a.closed {
		return
	}
	if id != 0 && a.images[id] == nil {
		id = 0
	}
	if a.x11 != nil {
		if _, ok := a.x11.Window(id); ok {
			a.remember(a.x11.Focus(id))
		} else {
			a.remember(a.x11.Focus(0))
		}
	}
	a.remember(a.server.Focus(id))
}
func (a *applicationController) Resize(id uint64, width, height int) {
	if a.closed {
		return
	}
	if a.images[id] == nil {
		return
	}
	if a.x11 != nil {
		if _, ok := a.x11.Window(id); ok {
			a.remember(a.x11.Resize(id, width, height))
			return
		}
	}
	a.remember(a.server.Resize(id, width, height))
}

func (a *applicationController) CloseApplication(id uint64) {
	if a.closed {
		return
	}
	if a.x11 != nil {
		if _, ok := a.x11.Window(id); ok {
			a.remember(a.x11.CloseSurface(id))
			return
		}
	}
	a.remember(a.server.CloseSurface(id))
}

func (a *applicationController) seat(event experience.Event) {
	if a.closed {
		return
	}
	switch event.Kind {
	case experience.KeymapChanged:
		a.remember(a.server.SetKeymap(event.Keymap))
	case experience.KeyboardRepeatInfo:
		a.remember(a.server.SetRepeat(event.RepeatRate, event.RepeatDelay))
	case experience.KeyboardModifiers, experience.KeyInput:
		// Retain seat state even while the workspace owns the keyboard: a held
		// modifier must already be correct when the next click focuses a client.
		a.remember(a.server.Modifiers(event.Depressed, event.Latched, event.Locked, event.Group))
	case experience.KeyboardCancel:
		a.remember(a.server.Modifiers(0, 0, 0, 0))
	}
}

func (a *applicationController) Seat(event experience.Event) { a.seat(event) }

func (a *applicationController) Send(id uint64, event experience.Event) {
	if a.closed {
		return
	}
	entry := a.images[id]
	if entry == nil {
		return // A disconnect can precede the experience's next layout update.
	}
	motion := func() {
		w, h := entry.surface.Texture.Size()
		a.remember(a.server.Pointer(id, event.X*float32(entry.logicalW)/float32(w), event.Y*float32(entry.logicalH)/float32(h)))
	}
	switch event.Kind {
	case experience.PointerMove:
		motion()
	case experience.PointerDown, experience.PointerUp:
		motion()
		code := event.ButtonCode
		if code == 0 {
			switch event.Button {
			case experience.ButtonPrimary:
				code = 0x110
			case experience.ButtonSecondary:
				code = 0x111
			case experience.ButtonMiddle:
				code = 0x112
			}
		}
		if code != 0 {
			a.remember(a.server.Button(code, event.Kind == experience.PointerDown, event.Time))
		}
	case experience.PointerScroll:
		motion()
		a.remember(a.server.Axis(event.ScrollX, event.ScrollY, event.Time))
	case experience.PointerCancel:
		a.remember(a.server.Pointer(0, 0, 0))
	case experience.KeyInput:
		if event.Keycode != 0 && !event.Repeat {
			a.remember(a.server.Key(event.Keycode, event.Pressed, event.Time, event.Depressed, event.Latched, event.Locked, event.Group))
		}
	case experience.KeyboardModifiers:
		a.remember(a.server.Modifiers(event.Depressed, event.Latched, event.Locked, event.Group))
	case experience.KeyboardCancel:
		a.Focus(0)
	}
}

// Poll, Close and RetiredTextures integrate compatibility content with the
// same provider lifecycle as native applications.
func (a *applicationController) Poll() error { return a.poll() }

func (a *applicationController) Close() error {
	a.close()
	return a.closeErr
}

func (a *applicationController) RetiredTextures() []uint64 {
	retired := a.retired
	a.retired = nil
	return retired
}

func (a *applicationController) close() {
	if a.closed {
		return
	}
	a.closed = true
	if a.x11 != nil {
		for _, surface := range a.surfaces {
			if _, ok := a.x11.Window(surface.ID); ok {
				_ = a.x11.CloseSurface(surface.ID)
			}
		}
	}
	a.retireCursor()
	for _, entry := range a.images {
		a.retireApplicationImage(entry)
	}
	a.images = nil
	a.surfaces = nil
	// Give the client its normal close request while the display is still alive,
	// dispatching the final resource teardown before disconnecting the socket.
	_ = a.server.RequestClose()
	{
		deadline := time.Now().Add(400 * time.Millisecond)
		for time.Now().Before(deadline) {
			a.collectExits(false)
			if a.x11 != nil {
				_ = a.x11.Poll()
			}
			mapped, _ := a.server.Poll()
			if (a.exited || len(a.children) == 0) && len(mapped) == 0 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if a.x11 != nil {
		a.closeErr = errors.Join(a.closeErr, a.x11.Close())
	}
	a.closeErr = errors.Join(a.closeErr, a.server.Close())
	a.collectServerTextures()
	a.collectExits(false)
	for _, child := range a.children {
		if !child.reaped && child.observeErr == nil {
			stopApplicationProcess(child.cmd)
		}
	}
	deadline := time.Now().Add(400 * time.Millisecond)
	for len(a.children) > 0 && time.Now().Before(deadline) {
		a.collectExits(false)
		if !applicationProcessesRunning(a.children) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	for _, child := range a.children {
		if !child.reaped && child.observeErr == nil {
			killApplicationProcess(child.cmd)
		}
	}
	for _, child := range a.children {
		if !child.exited {
			child.observeErr = <-child.wait
			child.exited = true
			child.reaped = !applicationExitPinned()
		}
	}
	// Every group signal precedes reaping any leader: an unreaped leader pins
	// its numeric PID/PGID even when the launcher exited before its GUI child.
	// Wait concurrently so inherited output pipes cannot multiply WaitDelay.
	type reapResult struct {
		child *applicationProcess
		err   error
	}
	results := make(chan reapResult, len(a.children))
	pending := 0
	for _, child := range a.children {
		if !child.reaped {
			pending++
			go func() { results <- reapResult{child, child.cmd.Wait()} }()
		}
	}
	for i := 0; i < pending; i++ {
		result := <-results
		result.child.exitErr = result.err
		result.child.reaped = true
	}
	a.collectExits(false)
	if log := a.log.drain(); log != "" {
		fmt.Fprint(a.output, log)
	}
}

// exec copies child output from another goroutine. Keep it bounded and drain it
// on the host goroutine so even a bytes.Buffer output writer remains race-free.
type applicationLog struct {
	mu   sync.Mutex
	data []byte
}

func (l *applicationLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	const limit = 32 * 1024
	if space := limit - len(l.data); space > 0 {
		l.data = append(l.data, p[:min(len(p), space)]...)
	}
	return len(p), nil
}
func (l *applicationLog) drain() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := string(l.data)
	l.data = l.data[:0]
	return s
}
