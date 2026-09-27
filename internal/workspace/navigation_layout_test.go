package workspace

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func navigationLayoutFixture(t *testing.T, count int) (*Workspace, *fakeApplications) {
	t.Helper()
	w, err := NewNavigator()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	texture, err := render.NewTexture(960, 600, make([]byte, 960*600*4))
	if err != nil {
		t.Fatal(err)
	}
	apps := &fakeApplications{}
	for i := range count {
		apps.surfaces = append(apps.surfaces, experience.ApplicationSurface{ID: uint64(100 + i), Key: fmt.Sprintf("native:nav-%d", i), Title: fmt.Sprintf("App %d", i), Texture: texture})
	}
	w.SetApplications(apps)
	w.Draw(1280, 820)
	return w, apps
}

func TestNavigatorAnimatedRectsMatchProjectionAndClientPixels(t *testing.T) {
	w, apps := navigationLayoutFixture(t, 4)
	w.navigationProject(0)
	w.Update(time.Second)
	if err := w.ActivateApplication(apps.surfaces[1].Key); err != nil {
		t.Fatal(err)
	}
	w.layout(w.width, w.height)
	w.Update(130 * time.Millisecond)
	w.Draw(1280, 820)
	slot := w.m.applicationState.index(apps.surfaces[1].Key)
	b := w.navigation.motion.poses()[slot].bounds
	for _, p := range [][2]float32{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {.5, .5}} {
		x, y := projectedApplication(w, apps.surfaces[1], p[0]*960, p[1]*600)
		if abs(x-b.x-p[0]*b.w) > .02 || abs(y-b.y-p[1]*b.h) > .02 {
			t.Fatalf("projection diverged from animated rectangle: %v,%v vs %+v", x, y, b)
		}
	}
	e, actual, hit := w.mapApplication(experience.Event{X: b.x + b.w*.3, Y: b.y + b.h*.7}, false)
	if !hit || actual.ID != apps.surfaces[1].ID || math.Abs(float64(e.X-288)) > .03 || math.Abs(float64(e.Y-420)) > .03 {
		t.Fatalf("animated input mismatch: %+v %d %v", e, actual.ID, hit)
	}
}

func TestNavigatorLayoutStableSlotsAndBoundedLaptopPages(t *testing.T) {
	w, apps := navigationLayoutFixture(t, 32)
	w.navigationProject(0)
	w.Update(time.Second)
	for _, size := range [][2]int{{1280, 820}, {1024, 768}, {640, 480}} {
		w.Draw(size[0], size[1])
		l := w.navigation.layout
		if l.pages < 2 {
			t.Fatal("32 apps must be paginated")
		}
		for page := 0; page < l.pages; page++ {
			w.navigation.page = page
			w.Draw(size[0], size[1])
			w.Update(time.Second)
			for _, card := range w.navigation.layout.apps {
				if card.bounds.w == 0 {
					continue
				}
				b := card.content
				if b.x < 0 || b.y < 100 || b.x+b.w > float32(size[0])+.1 || b.y+b.h > float32(size[1])-20 {
					t.Fatalf("app escaped usable laptop area %v: %+v", size, b)
				}
			}
		}
	}
	// Provider reordering must not move an existing application to another slot.
	before := w.navigation.motion.target
	for i, j := 0, len(apps.surfaces)-1; i < j; i, j = i+1, j-1 {
		apps.surfaces[i], apps.surfaces[j] = apps.surfaces[j], apps.surfaces[i]
	}
	w.Draw(640, 480)
	if before != w.navigation.motion.target {
		t.Fatal("provider order changed navigation targets")
	}
	// Minimized apps remain represented and can be restored from an overview.
	key := apps.surfaces[0].Key
	_ = w.ActivateApplication(key)
	_ = w.Dispatch(Action{Kind: ToggleApplicationMinimized})
	w.navigationProject(0)
	_ = w.ActivateApplication(key)
	if w.application.Key != key || w.m.applicationState.Layouts[w.m.applicationState.index(key)].Minimized {
		t.Fatal("minimized app did not restore")
	}
}

func TestNavigatorSpatialObjectsRetainGeometryAndPicking(t *testing.T) {
	w, apps := navigationLayoutFixture(t, 2)
	mesh, err := scene.NewMesh([]scene.Vec3{{X: -.15, Y: -.1}, {X: .15, Y: -.1}, {Y: .15}}, []uint32{0, 1, 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	apps.surfaces[0].Spatial = &experience.SpatialContent{Objects: []experience.SpatialObject{{ID: 7, Node: scene.Node{Mesh: mesh, Transform: scene.Translate(0, 0, .02)}}}}
	w.syncApplications()
	_ = w.ActivateApplication(apps.surfaces[0].Key)
	w.layout(w.width, w.height)
	w.Update(time.Second)
	frame := w.Draw(1280, 820)
	retained := false
	for _, c := range frame.Commands {
		for _, d := range c.Draws {
			if d.Geometry == mesh.Geometry() {
				retained = true
			}
		}
	}
	if !retained {
		t.Fatal("native app geometry was replaced by a screenshot")
	}
	p := w.spatialApplicationTransform(apps.surfaces[0]).TransformPoint(scene.Vec3{Z: .02})
	x, y, _, _ := w.camera.Project(p, w.viewport)
	mapped, s, ok := w.mapApplication(experience.Event{X: x, Y: y}, false)
	if !ok || s.ID != apps.surfaces[0].ID || mapped.SpatialObject != 7 {
		t.Fatalf("native object lost input identity: %+v %+v", mapped, s)
	}
	w.navigationProject(0)
	w.Update(time.Second)
	w.syncScene()
	if _, ok := w.applicationHit(x, y); ok {
		t.Fatal("project preview leaked through navigation to native app")
	}
}

func TestNavigatorResizingSnapsAndMotionOffIsExact(t *testing.T) {
	w, apps := navigationLayoutFixture(t, 2)
	_ = w.ActivateApplication(apps.surfaces[0].Key)
	w.layout(w.width, w.height)
	w.Update(70 * time.Millisecond)
	if !w.navigation.motion.moving() {
		t.Fatal("missing transition")
	}
	w.Draw(1024, 768)
	if w.navigation.motion.moving() {
		t.Fatal("resize continued interpolating old framebuffer coordinates")
	}
	w.navigation.motionEnabled = false
	w.navigationProject(0)
	if w.navigation.motion.moving() || w.navigation.motion.current != w.navigation.motion.target {
		t.Fatal("disabled motion did not immediately reach exact layout")
	}
}

func TestNavigatorHiDPIKeepsNavigationProportions(t *testing.T) {
	w, _ := navigationLayoutFixture(t, 3)
	base := w.navigation.layout
	w.Draw(2240, 1435) // A 1280×820 host window at 175% scale.
	l := w.navigation.layout
	if abs(l.unit-1.75) > .001 {
		t.Fatal("navigation shrank on the scaled host output", l.unit)
	}
	for _, pair := range [][2]box{{base.home, l.home}, {base.tools, l.tools}, {base.content, l.content}} {
		a, b := pair[0], pair[1]
		if abs(b.x-a.x*1.75) > .01 || abs(b.y-a.y*1.75) > .01 || abs(b.w-a.w*1.75) > .01 || abs(b.h-a.h*1.75) > .01 {
			t.Fatalf("scaled navigation target lost proportions: %+v %+v", a, b)
		}
	}
}
