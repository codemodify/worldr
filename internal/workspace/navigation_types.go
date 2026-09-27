package workspace

import "github.com/codemodify/worldr/internal/experience"

type navigationLevel uint8

const (
	navigationHome navigationLevel = iota
	navigationProject
	navigationApp
)

// All navigation rectangles are framebuffer coordinates, shared by drawing and
// input. Stable layout slots, rather than transient provider order, own motion.
type navigationAppCard struct {
	slot              int
	id                uint64
	key, title        string
	bounds, content   box
	active, minimized bool
}
type navigationProjectCard struct {
	space  uint8
	name   string
	count  int
	bounds box
}
type navigationSpaceTab struct {
	space  uint8
	name   string
	bounds box
	active bool
}
type navigationTool struct {
	id, label string
	bounds    box
	disabled  bool
}
type navigationLayout struct {
	width, height, unit                                         float32
	back, home, overview, search, tools, motion, previous, next box
	pagePrevious, pageNext                                      box
	content, toolPanel                                          box
	projects                                                    []navigationProjectCard
	tabs                                                        []navigationSpaceTab
	apps                                                        []navigationAppCard
	toolItems                                                   []navigationTool
	page, pages                                                 int
}
type navigationState struct {
	home                  bool
	page                  int
	motionEnabled         bool
	motion                navigationMotion
	layout                navigationLayout
	level                 navigationLevel
	toolsOpen             bool
	toolProgress          float32
	selectedTool          int
	hover, pressed        string
	keyboard              string
	restoreAfterModal     bool
	projectPage, homePage int
	projectPages          [16]int
	pressX, pressY        float32
	pressButton           uint32
	held                  map[helpKey]bool
	returnFocus           string
	help                  bool
	lastWidth, lastHeight int
	demoStep              int
}

func NewNavigator() (*Workspace, error) {
	w, err := NewDesktop()
	if err != nil {
		return nil, err
	}
	w.navigation = &navigationState{home: true, motionEnabled: true, held: make(map[helpKey]bool), demoStep: -1}
	w.environment = environmentSettings{}
	return w, nil
}

func navigationKey(e experience.Event, key experience.Key, code uint32) bool {
	return e.Key == key || e.Key == experience.KeyUnknown && e.Keycode == code
}

func (w *Workspace) navigationLevel() navigationLevel {
	if w.navigation.home {
		return navigationHome
	}
	if w.m.applicationState.Reading && !w.m.applicationState.Overview && w.application.ID != 0 {
		return navigationApp
	}
	return navigationProject
}
