// Package mediaapp presents native video playback as retained workspace content.
package mediaapp

import (
	"fmt"
	"image"
	"os"
	"path/filepath"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/media"
)

type playback interface {
	Load(*os.File) error
	Poll() (media.State, error)
	Render([]byte, int, int, int) (bool, error)
	Pause(bool) error
	Seek(float64) error
	SetVolume(float64) error
	SetMute(bool) error
	Close() error
}

// Manager owns one player window. Opening another file replaces its playback,
// retaining the stable workspace placement and avoiding overlapping audio.
// All methods belong to the host goroutine; libmpv owns its decode/audio threads.
type Manager struct {
	options                media.Options
	factory                func(media.Options) (playback, error)
	player                 playback
	file                   *os.File
	renderer               *playerRenderer
	video                  *image.RGBA
	state                  media.State
	title, message         string
	next                   uint64
	surfaces               []experience.ApplicationSurface
	retired                []uint64
	focused, dirty, closed bool
	pressed                string
	seekX                  float32
	resume                 *pendingResume
}

var _ experience.Applications = (*Manager)(nil)
var _ experience.ApplicationCloser = (*Manager)(nil)

func NewManager(options media.Options) *Manager {
	return &Manager{options: options, factory: func(o media.Options) (playback, error) { return media.New(o) }}
}

// OpenFile takes ownership only on success. The descriptor remains open until
// the decoder is closed, so playback follows the opened file through renames.
func (m *Manager) OpenFile(file *os.File, name string) (string, error) {
	return m.openFile(file, name, nil)
}

func (m *Manager) openFile(file *os.File, name string, restore *SessionState) (string, error) {
	if m.closed {
		return "", fmt.Errorf("media player is closed")
	}
	if file == nil {
		return "", fmt.Errorf("no video file")
	}
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("only regular video files can be played")
	}
	if m.next == ^uint64(0) {
		return "", fmt.Errorf("media player IDs exhausted")
	}
	options := m.options
	if restore != nil {
		// A restore always loads paused; seek completion releases this gate.
		options.InitialState = &media.InitialState{Paused: true, Volume: restore.Volume, Muted: restore.Muted}
	}
	player, err := m.factory(options)
	if err != nil {
		return "", err
	}
	r, err := newPlayerRenderer(1120, 700)
	if err != nil {
		_ = player.Close()
		return "", err
	}
	if err = player.Load(file); err != nil {
		r.close()
		_ = player.Close()
		return "", err
	}
	if err = r.paint(playerView{Title: name}, nil); err != nil {
		r.close()
		_ = player.Close()
		return "", err
	}
	m.closeCurrent()
	m.player, m.file, m.renderer = player, file, r
	m.title, m.message = cleanTitle(filepath.Base(name)), ""
	m.state = media.State{Volume: 70}
	if options.InitialState != nil {
		m.state.Paused, m.state.Volume, m.state.Muted = options.InitialState.Paused, options.InitialState.Volume, options.InitialState.Muted
	}
	if restore != nil {
		m.resume = &pendingResume{state: *restore}
	}
	m.next++
	m.surfaces = []experience.ApplicationSurface{{ID: m.next, Key: "native:media-player", AppID: "worldr.media-player", Title: "Media / " + m.title, Texture: r.texture}}
	m.dirty = true
	return "native:media-player", nil
}

func (m *Manager) closeCurrent() {
	if m.player != nil {
		_ = m.player.Close()
		m.player = nil
	}
	if m.file != nil {
		_ = m.file.Close()
		m.file = nil
	}
	if m.renderer != nil {
		m.retired = append(m.retired, m.renderer.texture.ID())
		m.renderer.close()
		m.renderer = nil
	}
	m.video = nil
	m.surfaces = nil
	m.pressed = ""
	m.focused = false
	m.resume = nil
}
func (m *Manager) Close() error {
	if !m.closed {
		m.closeCurrent()
		m.closed = true
	}
	return nil
}
func (m *Manager) Surfaces() []experience.ApplicationSurface { return m.surfaces }
func (m *Manager) RetiredTextures() []uint64                 { ids := m.retired; m.retired = nil; return ids }
func (m *Manager) CloseApplication(id uint64) {
	if m.valid(id) {
		m.closeCurrent()
	}
}
func (m *Manager) valid(id uint64) bool { return !m.closed && m.player != nil && id == m.next }
func (m *Manager) Focus(id uint64) {
	focused := m.valid(id)
	if focused != m.focused {
		m.focused, m.dirty = focused, true
	}
	if !focused {
		m.pressed = ""
	}
}
func (m *Manager) Seat(e experience.Event) {
	if e.Kind == experience.KeyboardCancel {
		m.Focus(0)
	}
}

func (m *Manager) remember(err error) {
	if err != nil && m.message != err.Error() {
		m.message = err.Error()
		m.dirty = true
	}
}
func (m *Manager) view() playerView {
	v := playerView{Title: m.title, Message: m.message, Loaded: m.state.Loaded, Paused: m.state.Paused, Ended: m.state.Ended, Muted: m.state.Muted, Focused: m.focused, Position: m.state.Position, Duration: m.state.Duration, Volume: m.state.Volume, VideoWidth: m.state.Width, VideoHeight: m.state.Height}
	if m.pressed == "seek" && v.Duration > 0 {
		r := m.renderer.layout().Seek
		v.Position = max(0, min(1, float64(m.seekX-float32(r.Min.X))/float64(r.Dx()))) * v.Duration
	}
	return v
}
func (m *Manager) paint() error {
	if m.renderer == nil {
		return nil
	}
	video := m.video
	r := m.renderer.layout().Video
	if video != nil && (video.Rect.Dx() != r.Dx() || video.Rect.Dy() != r.Dy()) {
		video = nil
	}
	err := m.renderer.paint(m.view(), video)
	m.dirty = false
	return err
}
func (m *Manager) Poll() error {
	if m.player == nil || m.closed {
		return nil
	}
	state, err := m.player.Poll()
	m.remember(err)
	if err == nil {
		m.resumePlayback(&state)
	}
	if state != m.state {
		m.state = state
		m.dirty = true
	}
	if state.Error != "" && m.message != state.Error {
		m.message = state.Error
		m.dirty = true
	}
	rect := m.renderer.layout().Video
	if m.video == nil || m.video.Rect.Dx() != rect.Dx() || m.video.Rect.Dy() != rect.Dy() {
		m.video = image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
		m.dirty = true
	}
	if state.Loaded && state.HasVideo {
		changed, err := m.player.Render(m.video.Pix, rect.Dx(), rect.Dy(), m.video.Stride)
		m.remember(err)
		m.dirty = m.dirty || changed
	}
	if m.dirty {
		return m.paint()
	}
	return nil
}
func (m *Manager) Resize(id uint64, width, height int) {
	if !m.valid(id) {
		return
	}
	width = max(minWidth, min(maxWidth, width))
	height = max(minHeight, min(maxHeight, height))
	if m.renderer.image.Rect.Dx() == width && m.renderer.image.Rect.Dy() == height {
		return
	}
	m.remember(m.renderer.resize(width, height))
	m.dirty = true
	m.pressed = ""
	m.remember(m.paint())
}

func (m *Manager) target(x, y float32) string {
	p := image.Pt(int(x), int(y))
	l := m.renderer.layout()
	for _, b := range []struct {
		name string
		rect image.Rectangle
	}{{"play", l.Play}, {"back", l.Back}, {"stop", l.Stop}, {"forward", l.Forward}, {"mute", l.Mute}, {"volume", l.Volume}, {"seek", l.Seek}, {"video", l.Video}} {
		if p.In(b.rect) {
			return b.name
		}
	}
	return ""
}
func (m *Manager) seek(x float32) {
	if m.state.Duration <= 0 {
		return
	}
	r := m.renderer.layout().Seek
	seconds := max(0, min(1, float64(x-float32(r.Min.X))/float64(r.Dx()))) * m.state.Duration
	m.remember(m.player.Seek(seconds))
	m.state.Position = seconds
	m.state.Ended = false
	m.dirty = true
}
func (m *Manager) volume(x float32) {
	r := m.renderer.layout().Volume
	v := max(0, min(100, float64(x-float32(r.Min.X))/float64(r.Dx())*100))
	m.remember(m.player.SetVolume(v))
	m.state.Volume = v
	m.dirty = true
}
func (m *Manager) togglePlay() {
	if m.state.Ended {
		m.remember(m.player.Seek(0))
		m.state.Ended = false
		m.state.Position = 0
		m.state.Paused = true
	}
	m.state.Paused = !m.state.Paused
	m.remember(m.player.Pause(m.state.Paused))
	m.dirty = true
}
func (m *Manager) action(target string) {
	if !m.state.Loaded {
		return
	}
	switch target {
	case "play", "video":
		m.togglePlay()
	case "stop":
		m.remember(m.player.Pause(true))
		m.remember(m.player.Seek(0))
		m.state.Paused = true
		m.state.Ended = false
		m.state.Position = 0
	case "back", "forward":
		delta := float64(10)
		if target == "back" {
			delta = -10
		}
		position := max(0, m.state.Position+delta)
		if m.state.Duration > 0 {
			position = min(position, m.state.Duration)
		}
		m.remember(m.player.Seek(position))
		m.state.Position = position
		m.state.Ended = false
	case "mute":
		m.state.Muted = !m.state.Muted
		m.remember(m.player.SetMute(m.state.Muted))
	}
	m.dirty = true
}
func (m *Manager) Send(id uint64, e experience.Event) {
	if !m.valid(id) {
		return
	}
	if m.resume != nil && e.Kind != experience.PointerCancel && e.Kind != experience.KeyboardCancel {
		return // The loading/seek transaction owns transport until it is ready.
	}
	switch e.Kind {
	case experience.PointerCancel:
		m.pressed = ""
		m.dirty = true
	case experience.KeyboardCancel:
		m.Focus(0)
	case experience.PointerDown:
		if e.Button != experience.ButtonPrimary && e.ButtonCode != 272 {
			return
		}
		m.pressed = m.target(e.X, e.Y)
		m.seekX = e.X
		m.dirty = true
		if m.pressed == "volume" {
			m.volume(e.X)
		}
	case experience.PointerMove:
		if m.pressed == "volume" {
			m.volume(e.X)
		}
		if m.pressed == "seek" {
			m.seekX = e.X
			m.dirty = true
		}
	case experience.PointerUp:
		if e.Button != experience.ButtonPrimary && e.ButtonCode != 272 {
			return
		}
		pressed := m.pressed
		m.pressed = ""
		if pressed == "seek" {
			m.seek(e.X)
		} else if pressed == "volume" {
			m.volume(e.X)
		} else if pressed != "" && pressed == m.target(e.X, e.Y) {
			m.action(pressed)
		}
	case experience.KeyInput:
		if !m.focused || !e.Pressed || e.Repeat || e.Modifiers != 0 {
			return
		}
		switch {
		case e.Key == experience.KeySpace || e.Keycode == 57:
			m.action("play")
		case e.Key == experience.KeyLeft || e.Keycode == 105:
			m.action("back")
		case e.Key == experience.KeyRight || e.Keycode == 106:
			m.action("forward")
		case e.Keycode == 50:
			m.action("mute")
		}
	}
}
