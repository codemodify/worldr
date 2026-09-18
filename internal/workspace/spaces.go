package workspace

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
)

// SpaceState remembers a named spatial context. The active space's camera and
// selection remain in the live view; switching stores them before installation.
// Applications keep running in other spaces, with one keyboard owner overall.
type SpaceState struct {
	Name     string      `json:"name,omitempty"`
	Camera   CameraState `json:"camera"`
	Active   string      `json:"active,omitempty"`
	Selected uint32      `json:"selected,omitempty"`
	Reading  bool        `json:"reading,omitempty"`
	Overview bool        `json:"overview,omitempty"`
}

func (v ApplicationViewState) spaceExists(id uint8) bool {
	return int(id) < len(v.Spaces) && (id == 0 || v.Spaces[id].Name != "")
}
func (v ApplicationViewState) spaceName(id uint8) string {
	if !v.spaceExists(id) {
		return ""
	}
	name := v.Spaces[id].Name
	if id == 0 && name == "" {
		name = "Main"
	}
	return name
}
func validSpaceName(name string) bool {
	if name == "" || len(name) > 96 || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
func (v ApplicationViewState) validateSpaces() error {
	if !v.spaceExists(v.Space) {
		return fmt.Errorf("active space does not exist")
	}
	names := map[string]bool{}
	for i, s := range v.Spaces {
		if i != 0 && s.Name == "" {
			if s != (SpaceState{}) {
				return fmt.Errorf("empty space contains state")
			}
			continue
		}
		name := v.spaceName(uint8(i))
		lower := strings.ToLower(name)
		if !validSpaceName(name) || names[lower] {
			return fmt.Errorf("invalid or duplicate space name")
		}
		names[lower] = true
		if !finite(float64(s.Camera.Yaw)) || !finite(float64(s.Camera.Pitch)) || abs(s.Camera.Pitch) > 1.1 || !finite(float64(s.Camera.Zoom)) || abs(s.Camera.Zoom) > .8 || !validCameraTarget(s.Camera) {
			return fmt.Errorf("invalid saved space camera")
		}
	}
	return nil
}

func validCameraTarget(camera CameraState) bool {
	return finite(float64(camera.TargetX)) && finite(float64(camera.TargetY)) && finite(float64(camera.TargetDepth)) &&
		abs(camera.TargetX) <= 100 && abs(camera.TargetY) <= 100 && abs(camera.TargetDepth) <= 40
}
func reduceSpace(d *Document, a Action) error {
	v := &d.View.Application
	switch a.Kind {
	case CreateSpace:
		if !validSpaceName(a.SpaceName) {
			return fmt.Errorf("space needs a name of at most 96 bytes")
		}
		slot := -1
		for i := 1; i < len(v.Spaces); i++ {
			if v.Spaces[i].Name == "" {
				slot = i
				break
			}
		}
		if slot < 0 {
			return fmt.Errorf("workspace has reached its 16-space limit")
		}
		for i := range v.Spaces {
			if strings.EqualFold(v.spaceName(uint8(i)), a.SpaceName) {
				return fmt.Errorf("space name already exists")
			}
		}
		v.Spaces[slot] = SpaceState{Name: a.SpaceName, Camera: initialModel().document().View.Camera}
	case RenameSpace:
		if !v.spaceExists(a.Space) || !validSpaceName(a.SpaceName) {
			return fmt.Errorf("invalid space or name")
		}
		for i := range v.Spaces {
			if uint8(i) != a.Space && strings.EqualFold(v.spaceName(uint8(i)), a.SpaceName) {
				return fmt.Errorf("space name already exists")
			}
		}
		v.Spaces[a.Space].Name = a.SpaceName
	case SwitchSpace:
		if !v.spaceExists(a.Space) {
			return fmt.Errorf("space does not exist")
		}
		if a.Space == v.Space {
			return nil
		}
		old := &v.Spaces[v.Space]
		old.Camera, old.Active, old.Selected, old.Reading, old.Overview = d.View.Camera, v.Active, v.Selected, v.Reading, v.Overview
		next := v.Spaces[a.Space]
		v.Space = a.Space
		d.View.Camera = next.Camera
		v.Active, v.Selected, v.Reading, v.Overview, v.Placing = next.Active, next.Selected, next.Reading, next.Overview, false
		for i, p := range v.Layouts {
			if p.Space != v.Space {
				v.Selected &^= 1 << i
			}
		}
		v.repairActive()
		if v.Active == "" {
			v.Reading = false
			v.Overview = false
		}
	case MoveToSpace:
		if !v.spaceExists(a.Space) {
			return fmt.Errorf("space does not exist")
		}
		mask := v.movementSelection()
		for i := range v.Layouts {
			if mask&(1<<i) != 0 {
				v.Layouts[i].Space = a.Space
				if a.Space != v.Space {
					v.Selected &^= 1 << i
				}
			}
		}
		v.repairActive()
		if v.Active == "" {
			v.Reading = false
			v.Overview = false
		}
	}
	v.aliases()
	return nil
}
func (w *Workspace) inCurrentSpace(surface experience.ApplicationSurface) bool {
	i := w.m.applicationState.index(surface.Key)
	return i >= 0 && w.m.applicationState.Layouts[i].Space == w.m.applicationState.Space
}
func (w *Workspace) visibleApplications() []experience.ApplicationSurface {
	var result []experience.ApplicationSurface
	for _, s := range w.applicationSurfaces {
		if w.inCurrentSpace(s) {
			result = append(result, s)
		}
	}
	return result
}

// PlaceNewApplication places an explicitly opened new instance in the current
// space. Restoration never calls this: saved windows keep their original space.
// Reusing a closed launch slot preserves its size/position but leaves any old
// group, so a new terminal cannot silently move windows in a different project.
func (w *Workspace) PlaceNewApplication(key string) {
	w.syncApplications()
	d := w.Document()
	v := &d.View.Application
	i := v.index(key)
	if i < 0 || v.Layouts[i].Space == v.Space {
		return
	}
	w.stopWindowThrowForKey(key)
	v.Layouts[i].Space, v.Layouts[i].Group = v.Space, 0
	for j := range v.Spaces {
		v.Spaces[j].Selected &^= 1 << i
		if v.Spaces[j].Active == key {
			v.Spaces[j].Active = ""
			v.Spaces[j].Reading = false
		}
	}
	w.install(d, false)
}
