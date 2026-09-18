package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/codemodify/worldr/internal/accessibility"
	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
)

type applicationSource struct {
	provider int
	id       uint64
}

// AccessibilitySnapshot maps provider-local semantic trees into the hub's
// public application IDs. Bounds remain in application pixels; a desktop-bus
// adapter can combine them with the workspace's spatial projection later.
func (h *applicationHub) AccessibilitySnapshot() accessibility.Snapshot {
	if h.closed {
		return accessibility.Snapshot{}
	}
	surfaces := h.Surfaces()
	result := accessibility.Snapshot{Applications: make([]accessibility.Application, 0, len(surfaces))}
	for _, surface := range surfaces {
		application := accessibility.Application{
			ID: surface.ID, Key: surface.Key, AppID: surface.AppID, Title: surface.Title,
			CoordinateSpace: "application-pixels", Focused: surface.ID == h.focused,
		}
		if application.Title == "" {
			application.Title = surface.AppID
		}
		if source, ok := h.routes[surface.ID]; ok {
			if provider, ok := h.providers[source.provider].(interface {
				Semantics(uint64) nativeui.SemanticTree
			}); ok {
				tree := provider.Semantics(source.id)
				application.FocusedNode = tree.FocusedID
				application.NativeSemantics = true
				application.Nodes = make([]accessibility.Node, 0, len(tree.Nodes))
				for _, node := range tree.Nodes {
					application.Nodes = append(application.Nodes, accessibility.Node{
						ID: node.ID, Role: string(node.Role), Label: node.Label, Value: node.Value,
						Description: node.Description, X: node.Bounds.Min.X, Y: node.Bounds.Min.Y,
						Width: node.Bounds.Dx(), Height: node.Bounds.Dy(), Disabled: node.Disabled, Selected: node.Selected,
					})
				}
			}
		}
		result.Applications = append(result.Applications, application)
	}
	return result
}

var errApplicationHubClosed = errors.New("application hub is closed")

// applicationHub gives native and compatibility applications one scene-facing
// identity space. Provider IDs remain private, and only one provider can own
// keyboard focus. Stable document keys pass through without runtime handles.
type applicationHub struct {
	providers []experience.Applications
	ids       map[applicationSource]uint64
	routes    map[uint64]applicationSource
	next      uint64
	focused   uint64
	surfaces  []experience.ApplicationSurface
	closed    bool
	closeErr  error
}

func newApplicationHub(providers ...experience.Applications) *applicationHub {
	return &applicationHub{providers: append([]experience.Applications(nil), providers...), ids: make(map[applicationSource]uint64), routes: make(map[uint64]applicationSource)}
}

// Poll gives every active provider one turn, even when an earlier provider
// fails. The first error retains its provider context and underlying cause.
func (h *applicationHub) Poll() error {
	if h.closed {
		return errApplicationHubClosed
	}
	var first error
	for index, provider := range h.providers {
		if poller, ok := provider.(experience.ApplicationPoller); ok {
			if err := poller.Poll(); err != nil && first == nil {
				first = fmt.Errorf("application provider %d (%T) poll: %w", index+1, provider, err)
			}
		}
	}
	return first
}

// Close disables routing before any provider is torn down. Every provider gets
// its teardown call even when another fails; repeated calls return the same
// combined result without repeating work.
func (h *applicationHub) Close() error {
	if h.closed {
		return h.closeErr
	}
	h.closed = true
	h.focused = 0
	h.surfaces = nil
	h.ids = nil
	h.routes = nil
	var failures []error
	for index := len(h.providers) - 1; index >= 0; index-- {
		provider := h.providers[index]
		if closer, ok := provider.(experience.ApplicationProviderCloser); ok {
			if err := closer.Close(); err != nil {
				failures = append(failures, fmt.Errorf("application provider %d (%T) close: %w", index+1, provider, err))
			}
		}
	}
	h.closeErr = errors.Join(failures...)
	return h.closeErr
}

// RetiredTextures drains all providers, including after Close. Deduplication
// covers this batch; the provider contract prevents IDs being returned again
// in later batches, so the hub need not retain an unbounded history of IDs.
func (h *applicationHub) RetiredTextures() []uint64 {
	var retired []uint64
	seen := make(map[uint64]struct{})
	for _, provider := range h.providers {
		if source, ok := provider.(experience.ApplicationTextureRetirer); ok {
			for _, id := range source.RetiredTextures() {
				if id == 0 {
					continue
				}
				if _, duplicate := seen[id]; duplicate {
					continue
				}
				seen[id] = struct{}{}
				retired = append(retired, id)
			}
		}
	}
	return retired
}

func (h *applicationHub) RetiredGeometryIDs() []uint64 {
	var result []uint64
	seen := map[uint64]bool{}
	for _, p := range h.providers {
		if r, ok := p.(experience.ApplicationGeometryRetirer); ok {
			for _, id := range r.RetiredGeometryIDs() {
				if id != 0 && !seen[id] {
					result = append(result, id)
					seen[id] = true
				}
			}
		}
	}
	return result
}

func (h *applicationHub) Surfaces() []experience.ApplicationSurface {
	if h.closed {
		return nil
	}
	h.surfaces = h.surfaces[:0]
	live := make(map[applicationSource]bool)
	for index, provider := range h.providers {
		for _, surface := range provider.Surfaces() {
			if surface.ID == 0 {
				continue
			}
			source := applicationSource{index, surface.ID}
			live[source] = true
			id := h.ids[source]
			if id == 0 {
				h.next++
				id = h.next
				h.ids[source] = id
				h.routes[id] = source
			}
			surface.ID = id
			h.surfaces = append(h.surfaces, surface)
		}
	}
	for source, id := range h.ids {
		if !live[source] {
			if h.focused == id {
				h.providers[source.provider].Focus(0)
				h.focused = 0
			}
			delete(h.ids, source)
			delete(h.routes, id)
		}
	}
	return h.surfaces
}

func (h *applicationHub) Focus(id uint64) {
	if h.closed {
		return
	}
	target, ok := h.routes[id]
	if !ok {
		id = 0
	}
	for index, provider := range h.providers {
		local := uint64(0)
		if id != 0 && index == target.provider {
			local = target.id
		}
		provider.Focus(local)
	}
	h.focused = id
}
func (h *applicationHub) Send(id uint64, event experience.Event) {
	if h.closed {
		return
	}
	if source, ok := h.routes[id]; ok {
		if event.Kind == experience.TextCommit || event.Kind == experience.TextPreedit {
			state := h.TextInput(id)
			if !state.Enabled || state.ContextID != event.TextContext {
				return
			}
			event.TextContext = strings.TrimPrefix(event.TextContext, fmt.Sprintf("%d/", id))
		}
		h.providers[source.provider].Send(source.id, event)
	}
}
func (h *applicationHub) Resize(id uint64, width, height int) {
	if h.closed {
		return
	}
	if source, ok := h.routes[id]; ok {
		h.providers[source.provider].Resize(source.id, width, height)
	}
}

func (h *applicationHub) ApplicationCursor(id uint64) (experience.ApplicationCursor, bool) {
	if h.closed {
		return experience.ApplicationCursor{}, false
	}
	if source, ok := h.routes[id]; ok {
		if provider, ok := h.providers[source.provider].(experience.ApplicationCursorProvider); ok {
			return provider.ApplicationCursor(source.id)
		}
	}
	return experience.ApplicationCursor{}, false
}

func (h *applicationHub) CloseApplication(id uint64) {
	if h.closed {
		return
	}
	if source, ok := h.routes[id]; ok {
		if closer, ok := h.providers[source.provider].(experience.ApplicationCloser); ok {
			closer.CloseApplication(source.id)
		}
	}
}

// applicationLaunchCapacity lets a provider distinguish a launch which reuses
// one of its live surfaces from a launch which creates another surface. The
// hub's fallback is deliberately conservative: providers which do not expose
// this preflight are treated as adding a surface.
//
// Calls and launches run consecutively on the host goroutine, so the answer
// cannot race another launch through the same hub.
type applicationLaunchCapacity interface {
	ApplicationLaunchAddsSurface(kind string) bool
}

func (h *applicationHub) LaunchApplication(kind string) (string, error) {
	if h.closed {
		return "", errApplicationHubClosed
	}
	for _, provider := range h.providers {
		if launcher, ok := provider.(experience.ApplicationLauncher); ok {
			if catalog, ok := provider.(experience.ApplicationLaunchCatalog); ok {
				supported := false
				for _, choice := range catalog.ApplicationLaunches() {
					supported = supported || choice.Kind == kind
				}
				if !supported {
					continue
				}
			}
			addsSurface := true
			if capacity, ok := provider.(applicationLaunchCapacity); ok {
				addsSurface = capacity.ApplicationLaunchAddsSurface(kind)
			}
			if addsSurface && len(h.Surfaces()) >= 32 {
				return "", fmt.Errorf("workspace is full: close a window before opening another tool")
			}
			return launcher.LaunchApplication(kind)
		}
	}
	return "", fmt.Errorf("no native application launcher is available")
}

func (h *applicationHub) ApplicationLaunches() []experience.ApplicationLaunch {
	var result []experience.ApplicationLaunch
	seen := map[string]bool{}
	for _, provider := range h.providers {
		if catalog, ok := provider.(experience.ApplicationLaunchCatalog); ok {
			for _, choice := range catalog.ApplicationLaunches() {
				if choice.Kind != "" && !seen[choice.Kind] {
					result = append(result, choice)
					seen[choice.Kind] = true
				}
			}
		}
	}
	return result
}
func (h *applicationHub) seat(event experience.Event) {
	if h.closed {
		return
	}
	for _, provider := range h.providers {
		if seat, ok := provider.(interface{ Seat(experience.Event) }); ok {
			seat.Seat(event)
		}
	}
}

func (h *applicationHub) TextInput(id uint64) experience.TextInputState {
	if h.closed || h.focused != id {
		return experience.TextInputState{}
	}
	source, ok := h.routes[id]
	if !ok {
		return experience.TextInputState{}
	}
	provider, ok := h.providers[source.provider].(experience.ApplicationTextInput)
	if !ok {
		return experience.TextInputState{}
	}
	state := provider.TextInput(source.id)
	if state.Enabled {
		state.ContextID = fmt.Sprintf("%d/%s", id, state.ContextID)
	}
	return state
}

func (h *applicationHub) ApplicationDragActive(id uint64) bool {
	if h.closed {
		return false
	}
	source, ok := h.routes[id]
	if !ok {
		return false
	}
	router, ok := h.providers[source.provider].(experience.ApplicationDragRouter)
	return ok && router.ApplicationDragActive(source.id)
}
func (h *applicationHub) ApplicationDrag(sourceID, targetID uint64, event experience.Event) bool {
	if h.closed {
		return false
	}
	source, ok := h.routes[sourceID]
	if !ok {
		return false
	}
	router, ok := h.providers[source.provider].(experience.ApplicationDragRouter)
	if !ok {
		return false
	}
	target := uint64(0)
	if destination, ok := h.routes[targetID]; ok && destination.provider == source.provider {
		target = destination.id
	}
	return router.ApplicationDrag(source.id, target, event)
}
