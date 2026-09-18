package nativeapps

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/resourcepath"
	"github.com/codemodify/worldr/internal/terminal"
)

const maxNativeTerminals = 32

type managedTerminal struct {
	provider  *Provider
	id        uint64
	key       string
	directory string
}

// Manager owns independently focusable native terminals. Stable layout keys
// belong to reusable launch slots; runtime IDs never identify a later terminal.
// Like Provider, its methods belong to the host goroutine.
type Manager struct {
	options  Options
	factory  func(Options) (*Provider, error)
	slots    [maxNativeTerminals]*managedTerminal
	next     uint64
	focused  uint64
	surfaces []experience.ApplicationSurface
	retired  []uint64
	closed   bool
	err      error

	keymap, modifiers, repeat experience.Event
	copyText                  string
	copyReady                 bool
	pasteRequested            uint64
	pasteTarget               uint64
	pasteInFlight, pasteValid bool
}

var _ experience.Applications = (*Manager)(nil)
var _ experience.ApplicationCloser = (*Manager)(nil)

// NewManager starts no processes. Each explicit launch receives its own copy of
// the configured command arguments and environment.
func NewManager(options Options) *Manager {
	options.Args = slices.Clone(options.Args)
	options.Env = slices.Clone(options.Env)
	return &Manager{options: options, factory: New}
}

func (m *Manager) remember(err error) {
	if m.err == nil && err != nil {
		m.err = err
	}
}

func (m *Manager) LaunchApplication(kind string) (string, error) {
	if m.closed {
		return "", terminal.ErrClosed
	}
	if kind != "terminal" {
		return "", fmt.Errorf("unknown native application %q", kind)
	}
	return m.launchTerminal(m.options)
}

// NextTerminalKey allows the host to check saved-layout capacity before starting
// a process. The host must launch on the same goroutine, without another launch
// between the check and its corresponding call.
func (m *Manager) NextTerminalKey() (string, error) {
	slot, err := m.availableTerminalSlot()
	if err != nil {
		return "", err
	}
	return terminalSlotKey(slot), nil
}

// LaunchTerminalInDirectory borrows directory only while starting the child.
// It never changes the manager's default options, the parent's working directory,
// or input focus. The caller retains ownership on both success and failure.
func (m *Manager) LaunchTerminalInDirectory(directory *os.File) (string, error) {
	if m.closed {
		return "", terminal.ErrClosed
	}
	if err := validateTerminalDirectory(directory); err != nil {
		return "", err
	}
	options := m.options
	options.Directory = directory
	return m.launchTerminal(options)
}

func terminalSlotKey(slot int) string {
	if slot == 0 {
		return "native:terminal"
	}
	return fmt.Sprintf("native:terminal-%d", slot+1)
}

func (m *Manager) availableTerminalSlot() (int, error) {
	if err := m.prepareTerminalLaunch(); err != nil {
		return -1, err
	}
	for i, entry := range m.slots {
		if entry == nil {
			return i, nil
		}
	}
	return -1, fmt.Errorf("at most %d native terminals can be open", maxNativeTerminals)
}

func (m *Manager) prepareTerminalLaunch() error {
	if m.closed {
		return terminal.ErrClosed
	}
	// A close followed by a launch reuses its slot without losing retirement.
	m.detachClosed()
	if m.err != nil {
		return m.err
	}
	if m.next == ^uint64(0) {
		return fmt.Errorf("native application IDs exhausted")
	}
	return nil
}

func (m *Manager) launchTerminal(options Options) (string, error) {
	slot, err := m.availableTerminalSlot()
	if err != nil {
		return "", err
	}
	return m.launchTerminalAt(slot, options)
}

func (m *Manager) launchTerminalAt(slot int, options Options) (string, error) {
	// Capture the caller's borrowed launch directory before the factory returns.
	// It remains a fallback for simple backends without live cwd reporting.
	var directory string
	if options.Directory != nil {
		directory, _ = resourcepath.FromFile(options.Directory)
	} else if current, err := os.Open("."); err == nil {
		directory, _ = resourcepath.FromFile(current)
		_ = current.Close()
	}
	options.Args, options.Env = slices.Clone(options.Args), slices.Clone(options.Env)
	p, err := m.factory(options)
	if err != nil {
		return "", err
	}
	for _, event := range []experience.Event{m.keymap, m.modifiers, m.repeat} {
		if event.Kind != 0 {
			p.Seat(event)
		}
	}
	if err := p.Poll(); err != nil {
		_ = p.Close()
		return "", err
	}
	key := terminalSlotKey(slot)
	m.next++
	m.slots[slot] = &managedTerminal{provider: p, id: m.next, key: key, directory: directory}
	return key, nil
}

func (m *Manager) find(id uint64) *managedTerminal {
	if id != 0 {
		for _, entry := range m.slots {
			if entry != nil && entry.id == id && len(entry.provider.Surfaces()) != 0 {
				return entry
			}
		}
	}
	return nil
}

func (m *Manager) Surfaces() []experience.ApplicationSurface {
	m.surfaces = m.surfaces[:0]
	for _, entry := range m.slots {
		if entry == nil {
			continue
		}
		for _, surface := range entry.provider.Surfaces() {
			surface.ID, surface.Key = entry.id, entry.key
			m.surfaces = append(m.surfaces, surface)
		}
	}
	return m.surfaces
}

func (m *Manager) Focus(id uint64) {
	if m.find(id) == nil {
		id = 0
	}
	if m.focused != id {
		m.pasteRequested = 0
		if m.pasteInFlight {
			m.pasteValid = false
		}
	}
	m.focused = id
	for _, entry := range m.slots {
		if entry != nil {
			local := uint64(0)
			if entry.id == id {
				local = 1
			}
			entry.provider.Focus(local)
		}
	}
}

// Seat retains only keyboard metadata, never a physical key press. A newly
// launched terminal receives the current keymap before its modifier masks.
func (m *Manager) Seat(event experience.Event) {
	if m.closed {
		return
	}
	switch event.Kind {
	case experience.KeymapChanged:
		m.keymap = event
		m.modifiers = experience.Event{Kind: experience.KeyboardModifiers}
	case experience.KeyInput:
		event.Kind = experience.KeyboardModifiers
		m.modifiers = event
	case experience.KeyboardModifiers:
		m.modifiers = event
	case experience.KeyboardRepeatInfo:
		m.repeat = event
	case experience.KeyboardCancel:
		m.modifiers = experience.Event{Kind: experience.KeyboardModifiers}
		m.Focus(0)
	default:
		return
	}
	for _, entry := range m.slots {
		if entry != nil {
			entry.provider.Seat(event)
		}
	}
}

func (m *Manager) Send(id uint64, event experience.Event) {
	entry := m.find(id)
	if entry == nil {
		return
	}
	switch event.Kind {
	case experience.KeymapChanged, experience.KeyboardModifiers, experience.KeyboardRepeatInfo:
		m.Seat(event)
		return
	case experience.KeyboardCancel:
		if id == m.focused {
			m.Focus(0)
		}
	}
	entry.provider.Send(1, event)
	// Drain requests in actual input order, rather than terminal slot order.
	if text, ok := entry.provider.TakeCopy(); ok {
		m.copyText, m.copyReady = text, true
	}
	if entry.provider.TakePasteRequest() && !m.pasteInFlight && id == m.focused {
		m.pasteRequested = id
	}
}

func (m *Manager) Resize(id uint64, width, height int) {
	if entry := m.find(id); entry != nil {
		entry.provider.Resize(1, width, height)
	}
}

func (m *Manager) CloseApplication(id uint64) {
	if entry := m.find(id); entry != nil {
		if id == m.focused {
			m.Focus(0)
		}
		entry.provider.CloseApplication(1)
	}
}

func (m *Manager) detachClosed() {
	for i, entry := range m.slots {
		if entry == nil {
			continue
		}
		m.retired = append(m.retired, entry.provider.Retired()...)
		if len(entry.provider.Surfaces()) == 0 {
			m.remember(entry.provider.Poll())
			if m.focused == entry.id {
				m.Focus(0)
			}
			if m.pasteRequested == entry.id {
				m.pasteRequested = 0
			}
			if m.pasteTarget == entry.id {
				m.pasteValid = false
			}
			m.slots[i] = nil
		}
	}
}

func (m *Manager) Poll() error {
	if m.closed {
		return terminal.ErrClosed
	}
	for _, entry := range m.slots {
		if entry != nil {
			m.remember(entry.provider.Poll())
		}
	}
	m.detachClosed()
	return m.err
}

// RetiredTextures participates in the common provider resource lifecycle.
func (m *Manager) RetiredTextures() []uint64 { return m.Retired() }

func (m *Manager) Retired() []uint64 {
	retired := m.retired
	m.retired = nil
	return retired
}

func (m *Manager) TakeCopy() (string, bool) {
	text, ok := m.copyText, m.copyReady
	m.copyText, m.copyReady = "", false
	return text, ok
}

// TakePasteRequest leases one original requester until Paste completes or
// cancels it. Later requests cannot retarget an in-flight clipboard transfer.
func (m *Manager) TakePasteRequest() bool {
	if m.closed || m.pasteInFlight || m.pasteRequested == 0 {
		return false
	}
	id := m.pasteRequested
	m.pasteRequested = 0
	if id != m.focused || m.find(id) == nil {
		return false
	}
	m.pasteTarget, m.pasteInFlight, m.pasteValid = id, true, true
	return true
}

// Paste("") ends a cancelled or empty transfer. Losing focus invalidates the
// lease permanently, including when focus returns before the data arrives.
func (m *Manager) Paste(text string) error {
	id, valid := m.pasteTarget, m.pasteInFlight && m.pasteValid
	m.pasteTarget, m.pasteInFlight, m.pasteValid = 0, false, false
	if text == "" || !valid || m.closed || id != m.focused {
		return nil
	}
	if entry := m.find(id); entry != nil {
		return entry.provider.Paste(text)
	}
	return nil
}

func (m *Manager) Close() error {
	if m.closed {
		return nil
	}
	m.Focus(0)
	m.detachClosed()
	m.closed = true
	m.pasteRequested, m.pasteTarget = 0, 0
	m.pasteInFlight, m.pasteValid = false, false
	m.copyText, m.copyReady = "", false
	errs := []error{m.err}
	for i, entry := range m.slots {
		if entry == nil {
			continue
		}
		for _, surface := range entry.provider.Surfaces() {
			m.retired = append(m.retired, surface.Texture.ID())
		}
		m.retired = append(m.retired, entry.provider.Retired()...)
		if err := entry.provider.Close(); err != nil {
			errs = append(errs, err)
		}
		m.slots[i] = nil
	}
	m.surfaces = nil
	return errors.Join(errs...)
}
