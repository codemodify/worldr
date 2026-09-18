package researchapp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func researchFixture(t *testing.T) (*Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "experiment.csv")
	if err := os.WriteFile(path, []byte("time,temperature,pressure\n0,20,100\n1,24,105\n2,22,103\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	t.Cleanup(func() { _ = m.Close() })
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.OpenFile(file, filepath.Base(path)); err != nil {
		file.Close()
		t.Fatal(err)
	}
	waitResearch(t, m)
	return m, path
}

func waitResearch(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := m.Poll(); err != nil {
			t.Fatal(err)
		}
		loading := false
		for _, v := range m.slots {
			loading = loading || v != nil && v.loading
		}
		if !loading {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("research worker did not finish")
}

func TestResearchWorkbenchLinksTableChartAndSpatialSelection(t *testing.T) {
	m, _ := researchFixture(t)
	v := m.slots[0]
	if v.dataset == nil || len(v.spatial.Objects) < 4 {
		t.Fatal("dataset or spatial points missing", v.message)
	}
	m.Focus(v.id)
	m.Send(v.id, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 80, Y: 155})
	if v.dragging {
		t.Fatal("table selection incorrectly started spatial orbit")
	}
	m.Send(v.id, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, SpatialObject: 4})
	m.Send(v.id, experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, SpatialObject: 4})
	if v.state.View.Selected != 2 || len(v.spatial.Labels) != 4 {
		t.Fatal("spatial selection did not update the linked observation", v.state.View.Selected)
	}
	revision := v.renderer.texture.Revision()
	m.Send(v.id, experience.Event{Kind: experience.KeyInput, Keycode: 103, Pressed: true})
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	if v.state.View.Selected != 1 || v.renderer.texture.Revision() <= revision {
		t.Fatal("keyboard selection did not repaint the table and chart")
	}
	surface := m.Surfaces()[0]
	if surface.Spatial == nil || surface.FrameStyle != experience.FrameCinematic || surface.AppID != "worldr.research-workbench" {
		t.Fatalf("research surface lost its native spatial contract: %+v", surface)
	}
	tree := m.Semantics(v.id)
	if len(tree.Nodes) != 8 || tree.Nodes[6].ID != "dataset-summary" || tree.Nodes[7].Value == "" {
		t.Fatal("research semantics omitted the linked dataset state", tree)
	}
}

func TestResearchWorkbenchRefreshesAndRestoresState(t *testing.T) {
	m, path := researchFixture(t)
	v := m.slots[0]
	clock := time.Now()
	m.now = func() time.Time { return clock }
	if err := os.WriteFile(path, []byte("time,temperature,pressure\n0,20,100\n1,24,105\n2,22,103\n3,30,110\n"), 0600); err != nil {
		t.Fatal(err)
	}
	clock = v.nextRefresh.Add(time.Millisecond)
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	waitResearch(t, m)
	if len(v.dataset.Rows) != 4 {
		t.Fatal("changed dataset did not refresh", len(v.dataset.Rows))
	}
	v.state.View.Selected, v.state.View.Yaw, v.state.View.Mode = 3, .7, "scatter"
	saved := m.SessionStates()[0]
	m.CloseApplication(v.id)
	if len(m.Surfaces()) != 0 || len(m.RetiredTextures()) != 1 {
		t.Fatal("closed dashboard retained its surface or texture")
	}
	saved.Key = "native:research-workbench-3"
	if _, err := m.Restore(saved); err != nil {
		t.Fatal(err)
	}
	waitResearch(t, m)
	restored := m.slots[2]
	if restored == nil || restored.state.View.Selected != 3 || restored.state.View.Yaw != .7 || restored.state.View.Mode != "scatter" {
		t.Fatalf("dashboard view state was not restored: %+v", restored)
	}
}

func TestExplicitResearchReloadDoesNotDependOnFileTimestamp(t *testing.T) {
	m, _ := researchFixture(t)
	v := m.slots[0]
	v.action("reload")
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	if !v.loading {
		t.Fatal("explicit reload skipped an unchanged source")
	}
	waitResearch(t, m)
	if v.dataset == nil || len(v.dataset.Rows) != 3 {
		t.Fatal("explicit reload lost the dataset")
	}
}

func TestClosingResearchDashboardDoesNotWaitForParser(t *testing.T) {
	m, _ := researchFixture(t)
	v := m.slots[0]
	v.loading = true
	v.result = make(chan loadResult, 1)
	done := make(chan struct{})
	go func() { m.CloseApplication(v.id); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closing a dashboard waited for its parser")
	}
	if len(m.retiring) != 1 {
		t.Fatal("pending parser was not retained safely")
	}
	v.result <- loadResult{}
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	if len(m.retiring) != 0 {
		t.Fatal("completed parser remained retained")
	}
}

func TestResearchWorkbenchRepeatedOpenCloseRetiresEveryResource(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "cycling.csv")
	if err := os.WriteFile(path, []byte("x,y,z\n1,2,3\n4,5,6\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	for cycle := 0; cycle < 32; cycle++ {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = m.OpenFile(file, filepath.Base(path)); err != nil {
			file.Close()
			t.Fatal(err)
		}
		waitResearch(t, m)
		surfaces := m.Surfaces()
		if len(surfaces) != 1 || len(surfaces[0].Spatial.Objects) < 6 {
			t.Fatal("cycle did not publish a complete dashboard", cycle)
		}
		m.CloseApplication(surfaces[0].ID)
		if retired := m.RetiredTextures(); len(retired) != 1 {
			t.Fatal("cycle did not retire exactly one texture", cycle, retired)
		}
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if retired := m.RetiredGeometryIDs(); len(retired) != 1 {
		t.Fatal("shared point geometry was not retired exactly once", retired)
	}
}
