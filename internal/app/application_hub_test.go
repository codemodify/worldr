package app

import (
	"image"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
)

type hubProvider struct {
	surfaces               []experience.ApplicationSurface
	focused, sent, resized uint64
	width, height          int
	seated                 int
	closed                 uint64
}

type semanticHubProvider struct{ hubProvider }

func (p *semanticHubProvider) Semantics(id uint64) nativeui.SemanticTree {
	if id != 5 {
		return nativeui.SemanticTree{}
	}
	return nativeui.SemanticTree{FocusedID: "query", Nodes: []nativeui.Node{
		{ID: "query", Role: nativeui.RoleTextField, Label: "Search", Value: "engine", Bounds: image.Rect(10, 20, 210, 50)},
		{ID: "plot", Role: nativeui.RoleImage, Label: "Research plot", Bounds: image.Rect(10, 60, 410, 300)},
		{ID: "document", Role: nativeui.RoleDocument, Label: "Observations", Bounds: image.Rect(420, 60, 700, 300)},
		{ID: "status", Role: nativeui.RoleStatus, Label: "Dataset ready", Bounds: image.Rect(10, 310, 700, 340)},
	}}
}

func TestApplicationHubAccessibilityUsesPublicIDsAndScopedSemantics(t *testing.T) {
	provider := &semanticHubProvider{hubProvider: hubProvider{surfaces: []experience.ApplicationSurface{{ID: 5, Key: "native:test", AppID: "worldr.test", Title: "Research panel"}}}}
	hub := newApplicationHub(provider)
	surface := hub.Surfaces()[0]
	hub.Focus(surface.ID)
	snapshot := hub.AccessibilitySnapshot()
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Applications) != 1 || snapshot.Applications[0].ID != surface.ID || !snapshot.Applications[0].Focused || !snapshot.Applications[0].NativeSemantics {
		t.Fatalf("application semantics: %+v", snapshot)
	}
	application := snapshot.Applications[0]
	if application.FocusedNode != "query" || len(application.Nodes) != 4 || application.Nodes[0].Width != 200 || application.Nodes[0].Value != "engine" || application.Nodes[1].Role != "image" || application.Nodes[2].Role != "document" || application.Nodes[3].Role != "status" {
		t.Fatalf("node semantics: %+v", application)
	}
}

func (p *hubProvider) Surfaces() []experience.ApplicationSurface { return p.surfaces }
func (p *hubProvider) Focus(id uint64)                           { p.focused = id }
func (p *hubProvider) Send(id uint64, _ experience.Event)        { p.sent = id }
func (p *hubProvider) Resize(id uint64, w, h int)                { p.resized, p.width, p.height = id, w, h }
func (p *hubProvider) Seat(_ experience.Event)                   { p.seated++ }
func (p *hubProvider) CloseApplication(id uint64)                { p.closed = id }

func TestApplicationHubKeepsProvidersAndFocusIndependent(t *testing.T) {
	a := &hubProvider{surfaces: []experience.ApplicationSurface{{ID: 1, Key: "legacy"}}}
	b := &hubProvider{surfaces: []experience.ApplicationSurface{{ID: 1, Key: "native:terminal"}}}
	h := newApplicationHub(a, b)
	surfaces := h.Surfaces()
	first, second := surfaces[0].ID, surfaces[1].ID
	if first == second || surfaces[0].Key != "legacy" || surfaces[1].Key != "native:terminal" {
		t.Fatal("provider identities collided")
	}
	h.Focus(first)
	h.Send(first, experience.Event{})
	if a.focused != 1 || a.sent != 1 || b.focused != 0 || b.sent != 0 {
		t.Fatal("legacy focus/input leaked to native app")
	}
	h.Focus(second)
	h.Send(second, experience.Event{})
	h.Resize(second, 1440, 900)
	if a.focused != 0 || b.focused != 1 || b.sent != 1 || b.resized != 1 || b.width != 1440 || b.height != 900 || a.resized != 0 {
		t.Fatal("native focus/input/resize routing failed")
	}
	h.seat(experience.Event{Kind: experience.KeyboardModifiers})
	if a.seated != 1 || b.seated != 1 {
		t.Fatal("inactive provider lost seat metadata")
	}
	h.CloseApplication(second)
	if b.closed != 1 || a.closed != 0 {
		t.Fatal("close request reached the wrong provider")
	}
	b.surfaces = nil
	if len(h.Surfaces()) != 1 || b.focused != 0 || h.focused != 0 {
		t.Fatal("disconnected provider retained keyboard focus")
	}
	b.sent = 0
	h.Send(second, experience.Event{})
	if b.sent != 0 {
		t.Fatal("stale route delivered input")
	}
	b.surfaces = []experience.ApplicationSurface{{ID: 1, Key: "native:terminal"}}
	surfaces = h.Surfaces()
	if surfaces[1].ID == second || surfaces[0].ID != first {
		t.Fatal("reconnection reused stale route or changed live sibling identity")
	}
}

type capacityHubProvider struct {
	hubProvider
	addsSurface bool
	launches    int
}

func (p *capacityHubProvider) ApplicationLaunches() []experience.ApplicationLaunch {
	return []experience.ApplicationLaunch{{Kind: "files", Title: "Open Files"}}
}

func (p *capacityHubProvider) ApplicationLaunchAddsSurface(kind string) bool {
	return kind == "files" && p.addsSurface
}

func (p *capacityHubProvider) LaunchApplication(kind string) (string, error) {
	p.launches++
	if p.addsSurface {
		p.surfaces = append(p.surfaces, experience.ApplicationSurface{ID: uint64(len(p.surfaces) + 1), Key: "new"})
	}
	return p.surfaces[0].Key, nil
}

func fullHubSurfaces() []experience.ApplicationSurface {
	surfaces := make([]experience.ApplicationSurface, 32)
	for i := range surfaces {
		surfaces[i] = experience.ApplicationSurface{ID: uint64(i + 1), Key: "live"}
	}
	return surfaces
}

func TestApplicationHubAllowsCapacityNeutralLaunchAtSurfaceLimit(t *testing.T) {
	provider := &capacityHubProvider{hubProvider: hubProvider{surfaces: fullHubSurfaces()}}
	hub := newApplicationHub(provider)
	key, err := hub.LaunchApplication("files")
	if err != nil || key != "live" || provider.launches != 1 || len(provider.surfaces) != 32 {
		t.Fatalf("capacity-neutral launch failed: key=%q err=%v launches=%d surfaces=%d", key, err, provider.launches, len(provider.surfaces))
	}
}

func TestApplicationHubRejectsSurfaceAddingLaunchAtLimit(t *testing.T) {
	provider := &capacityHubProvider{hubProvider: hubProvider{surfaces: fullHubSurfaces()}, addsSurface: true}
	hub := newApplicationHub(provider)
	key, err := hub.LaunchApplication("files")
	if key != "" || err == nil || !strings.Contains(err.Error(), "another tool") || provider.launches != 0 || len(provider.surfaces) != 32 {
		t.Fatalf("surface-adding launch crossed capacity: key=%q err=%v launches=%d surfaces=%d", key, err, provider.launches, len(provider.surfaces))
	}
}

type legacyCapacityHubProvider struct {
	hubProvider
	launches int
}

func (p *legacyCapacityHubProvider) LaunchApplication(string) (string, error) {
	p.launches++
	return "new", nil
}

func TestApplicationHubCapacityPreflightDefaultsToAddingSurface(t *testing.T) {
	provider := &legacyCapacityHubProvider{hubProvider: hubProvider{surfaces: fullHubSurfaces()}}
	hub := newApplicationHub(provider)
	if key, err := hub.LaunchApplication("anything"); key != "" || err == nil || provider.launches != 0 {
		t.Fatalf("provider without capacity preflight bypassed the limit: key=%q err=%v launches=%d", key, err, provider.launches)
	}
}
