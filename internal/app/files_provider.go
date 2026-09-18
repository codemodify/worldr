package app

import (
	"fmt"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/projectapp"
)

// filesProvider keeps a stable provider lifetime while Files is closed/reopened
// from the launcher. Runtime IDs never repeat; its copy source follows the
// current browser and closed textures survive until the host drains them.
type filesProvider struct {
	current           *projectapp.Provider
	root              string
	next              uint64
	retired           []uint64
	onOpen            func(*projectapp.Provider)
	check             func() error
	closed            bool
	surfaces          []experience.ApplicationSurface
	keymap, modifiers experience.Event
}

func (f *filesProvider) Surfaces() []experience.ApplicationSurface {
	f.surfaces = f.surfaces[:0]
	if f.current == nil {
		return nil
	}
	for _, surface := range f.current.Surfaces() {
		surface.ID = f.next
		f.surfaces = append(f.surfaces, surface)
	}
	return f.surfaces
}
func (f *filesProvider) Focus(id uint64) {
	if f.current != nil {
		if id == f.next {
			f.current.Focus(1)
		} else {
			f.current.Focus(0)
		}
	}
}
func (f *filesProvider) Send(id uint64, e experience.Event) {
	if f.current != nil && id == f.next {
		f.current.Send(1, e)
	}
}
func (f *filesProvider) Resize(id uint64, w, h int) {
	if f.current != nil && id == f.next {
		f.current.Resize(1, w, h)
	}
}
func (f *filesProvider) Poll() error {
	if f.current != nil {
		return f.current.Poll()
	}
	return nil
}
func (f *filesProvider) TakeCopy() (string, bool) {
	if f.current != nil {
		return f.current.TakeCopy()
	}
	return "", false
}
func (f *filesProvider) Seat(e experience.Event) {
	switch e.Kind {
	case experience.KeymapChanged:
		f.keymap = e
	case experience.KeyboardModifiers:
		f.modifiers = e
	case experience.KeyboardCancel:
		f.modifiers = experience.Event{}
	}
	if f.current != nil {
		f.current.Seat(e)
	}
}
func (f *filesProvider) CloseApplication(id uint64) {
	if f.current == nil || id != f.next {
		return
	}
	if state, ok := f.current.SessionState(); ok {
		f.root = state.Root
	}
	_ = f.current.Close()
	f.retired = append(f.retired, f.current.RetiredTextures()...)
	f.current = nil
}
func (f *filesProvider) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	if f.current == nil {
		return nil
	}
	err := f.current.Close()
	f.retired = append(f.retired, f.current.RetiredTextures()...)
	f.current = nil
	return err
}
func (f *filesProvider) RetiredTextures() []uint64 {
	if f.current != nil {
		f.retired = append(f.retired, f.current.RetiredTextures()...)
	}
	ids := f.retired
	f.retired = nil
	return ids
}
func (f *filesProvider) ApplicationLaunches() []experience.ApplicationLaunch {
	return []experience.ApplicationLaunch{
		{Kind: "files", Title: "Open Files"},
		{Kind: "photo", Title: "Open photo"},
		{Kind: "media", Title: "Open video"},
		{Kind: "model", Title: "Inspect 3D model"},
		{Kind: "research", Title: "Open research data"},
	}
}

func (f *filesProvider) ApplicationLaunchAddsSurface(kind string) bool {
	_, supported := filesLaunchIntent(kind)
	return !supported || f.current == nil || len(f.current.Surfaces()) == 0
}

func (f *filesProvider) LaunchApplication(kind string) (string, error) {
	intent, ok := filesLaunchIntent(kind)
	if f.closed || !ok {
		return "", fmt.Errorf("Files launcher is unavailable")
	}
	if f.current != nil && len(f.current.Surfaces()) != 0 {
		f.current.SetOpenIntent(intent)
		return "native:project-browser", nil
	}
	if f.check != nil {
		if err := f.check(); err != nil {
			return "", err
		}
	}
	provider, err := projectapp.New(f.root)
	if err != nil {
		return "", err
	}
	if f.current != nil {
		_ = f.current.Close()
		f.retired = append(f.retired, f.current.RetiredTextures()...)
	}
	f.current = provider
	f.next++
	provider.SetOpenIntent(intent)
	if f.keymap.Kind == experience.KeymapChanged {
		provider.Seat(f.keymap)
	}
	if f.modifiers.Kind == experience.KeyboardModifiers {
		provider.Seat(f.modifiers)
	}
	if f.onOpen != nil {
		f.onOpen(provider)
	}
	return "native:project-browser", nil
}

func filesLaunchIntent(kind string) (projectapp.OpenIntent, bool) {
	switch kind {
	case "files":
		return projectapp.OpenAny, true
	case "photo":
		return projectapp.OpenPhoto, true
	case "media":
		return projectapp.OpenVideo, true
	case "model":
		return projectapp.OpenModel, true
	case "research":
		return projectapp.OpenDataset, true
	default:
		return projectapp.OpenAny, false
	}
}

func (f *filesProvider) TakePasteRequest() bool {
	return f.current != nil && f.current.TakePasteRequest()
}
func (f *filesProvider) Paste(text string) error {
	if f.current == nil {
		return nil
	}
	return f.current.Paste(text)
}
func (f *filesProvider) TextInput(id uint64) experience.TextInputState {
	if f.current != nil && id == f.next {
		return f.current.TextInput(1)
	}
	return experience.TextInputState{}
}

func (f *filesProvider) Semantics(id uint64) nativeui.SemanticTree {
	if f.current != nil && id == f.next {
		return f.current.Semantics(1)
	}
	return nativeui.SemanticTree{}
}
