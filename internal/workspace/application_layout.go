package workspace

import (
	"fmt"
	"math/bits"
	"unicode"
	"unicode/utf8"
)

const (
	// Every built-in native renderer and both compatibility bridges accept this
	// common range, so the saved spatial dimensions always match the size the
	// provider can actually present.
	minApplicationWidth  = 720
	minApplicationHeight = 440
	maxApplicationWidth  = 1920
	maxApplicationHeight = 1080
	// Maximize fills the readable workspace while retaining reachable chrome.
	// Larger custom surfaces remain available through direct resizing.
	maximizedApplicationWidth  = 1440
	maximizedApplicationHeight = 900
)

func (v ApplicationViewState) index(key string) int {
	if key != "" {
		for i, p := range v.Layouts {
			if p.Key == key {
				return i
			}
		}
	}
	return -1
}

func validApplicationKey(key string) bool {
	if key == "" || len(key) > 256 || !utf8.ValidString(key) {
		return false
	}
	for _, r := range key {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (v ApplicationViewState) validate() error {
	if err := v.validateSpaces(); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for i, p := range v.Layouts {
		if p.Key == "" {
			if p != (ApplicationPlacement{}) || v.Selected&(1<<i) != 0 {
				return fmt.Errorf("empty application layout %d contains state", i)
			}
			continue
		}
		if !validApplicationKey(p.Key) || seen[p.Key] {
			return fmt.Errorf("invalid or duplicate application layout key %q", p.Key)
		}
		if !v.spaceExists(p.Space) {
			return fmt.Errorf("application belongs to an unknown space")
		}
		seen[p.Key] = true
		customSize := p.Width != 0 || p.Height != 0
		restoreSize := p.RestoreWidth != 0 || p.RestoreHeight != 0
		if !finite(float64(p.X)) || !finite(float64(p.Y)) || !finite(float64(p.Depth)) || abs(p.X) > 100 || abs(p.Y) > 100 || abs(p.Depth) > 40 || p.Group > MaxApplicationLayouts ||
			customSize && (p.Width < minApplicationWidth || p.Width > maxApplicationWidth || p.Height < minApplicationHeight || p.Height > maxApplicationHeight) ||
			restoreSize && (p.RestoreWidth < minApplicationWidth || p.RestoreWidth > maxApplicationWidth || p.RestoreHeight < minApplicationHeight || p.RestoreHeight > maxApplicationHeight) ||
			p.Maximized && (p.Width != maximizedApplicationWidth || p.Height != maximizedApplicationHeight) ||
			!p.Maximized && (restoreSize || p.RestoreWide) {
			return fmt.Errorf("application layout %q is out of bounds", p.Key)
		}
	}
	if v.Active != "" && v.index(v.Active) < 0 {
		return fmt.Errorf("active application has no saved layout")
	}
	if v.Overview && v.Placing || v.Placing && v.Reading {
		return fmt.Errorf("incompatible application view modes")
	}
	return nil
}

func (v *ApplicationViewState) aliases() {
	if i := v.index(v.Active); i >= 0 {
		v.Behind, v.Wide = v.Layouts[i].Depth < 0, v.Layouts[i].Wide
	}
}

func (v ApplicationViewState) movementSelection() uint32 {
	selected := v.Selected
	if selected == 0 {
		if i := v.index(v.Active); i >= 0 && v.Layouts[i].Space == v.Space {
			selected = 1 << i
		}
	}
	groups := uint64(0)
	for i, p := range v.Layouts {
		if p.Space != v.Space {
			selected &^= 1 << i
		}
		if selected&(1<<i) != 0 && p.Group != 0 {
			groups |= 1 << p.Group
		}
	}
	for i, p := range v.Layouts {
		if p.Space == v.Space && p.Group != 0 && groups&(1<<p.Group) != 0 {
			selected |= 1 << i
		}
	}
	return selected
}

// movementSelectionFor addresses the window under a direct gesture without
// changing keyboard focus or the user's multi-selection. Explicit groups still
// move together because they are one spatial arrangement.
func (v ApplicationViewState) movementSelectionFor(key string) uint32 {
	i := v.index(key)
	if i < 0 || v.Layouts[i].Space != v.Space {
		return 0
	}
	selected := uint32(1 << i)
	group := v.Layouts[i].Group
	if group != 0 {
		for j, p := range v.Layouts {
			if p.Space == v.Space && p.Group == group {
				selected |= 1 << j
			}
		}
	}
	return selected
}

func reduceApplicationView(v *ApplicationViewState, a Action) error {
	active := v.index(v.Active)
	switch a.Kind {
	case ForgetClosedPlacements:
		for i := range v.Layouts {
			if a.ClosedPlacements&(1<<i) != 0 {
				v.Layouts[i] = ApplicationPlacement{}
				v.Selected &^= 1 << i
			}
		}
		v.repairActive()
	case SelectApplication:
		i := v.index(a.ApplicationKey)
		if i < 0 {
			return fmt.Errorf("unknown application layout %q", a.ApplicationKey)
		}
		if v.Layouts[i].Space != v.Space {
			return fmt.Errorf("application is in another space")
		}
		v.Layouts[i].Minimized = false
		v.Active = a.ApplicationKey
		if a.Additive {
			v.Selected ^= 1 << i
		} else {
			v.Selected = 1 << i
		}
	case ToggleApplicationReading:
		v.Reading = !v.Reading
		v.Overview, v.Placing = false, false
	case ToggleApplicationOverview:
		v.Overview = !v.Overview
		v.Placing = false
	case ToggleApplicationPlacement:
		v.Placing = !v.Placing
		v.Overview, v.Reading = false, false
	case ToggleApplicationSize:
		if active < 0 {
			v.Wide = !v.Wide
		} else {
			v.Layouts[active].Wide = !v.Layouts[active].Wide
			v.Layouts[active].Width, v.Layouts[active].Height = 0, 0
			v.Layouts[active].Maximized = false
			v.Layouts[active].RestoreWidth, v.Layouts[active].RestoreHeight, v.Layouts[active].RestoreWide = 0, 0, false
		}
	case ToggleApplicationMinimized:
		i := v.index(a.ApplicationKey)
		if i < 0 || v.Layouts[i].Space != v.Space {
			return fmt.Errorf("unknown application layout %q", a.ApplicationKey)
		}
		v.Layouts[i].Minimized = !v.Layouts[i].Minimized
		if v.Layouts[i].Minimized {
			v.Selected &^= 1 << i
			if v.Active == a.ApplicationKey {
				v.Active = ""
				v.Reading = false
			}
		} else {
			v.Active, v.Selected = a.ApplicationKey, 1<<i
		}
	case ToggleApplicationMaximized:
		i := v.index(a.ApplicationKey)
		if i < 0 || v.Layouts[i].Space != v.Space {
			return fmt.Errorf("unknown application layout %q", a.ApplicationKey)
		}
		p := &v.Layouts[i]
		p.Minimized = false
		if p.Maximized {
			p.Width, p.Height, p.Wide = p.RestoreWidth, p.RestoreHeight, p.RestoreWide
			p.Maximized = false
			p.RestoreWidth, p.RestoreHeight, p.RestoreWide = 0, 0, false
		} else {
			p.RestoreWidth, p.RestoreHeight, p.RestoreWide = p.Width, p.Height, p.Wide
			p.Width, p.Height, p.Wide, p.Maximized = maximizedApplicationWidth, maximizedApplicationHeight, true, true
		}
		v.Active, v.Selected = a.ApplicationKey, 1<<i
	case ToggleApplicationDepth:
		if active < 0 {
			v.Behind = !v.Behind
			break
		}
		target := float32(-1.6)
		if v.Layouts[active].Depth < 0 {
			target = 1.9
		}
		a.Kind, a.DeltaDepth = MoveApplications, target-v.Layouts[active].Depth
		return reduceApplicationView(v, a)
	case MoveApplications:
		if !finite(float64(a.DeltaX)) || !finite(float64(a.DeltaY)) || !finite(float64(a.DeltaDepth)) {
			return fmt.Errorf("application movement must be finite")
		}
		selected := v.movementSelection()
		for i := range v.Layouts {
			if selected&(1<<i) != 0 {
				v.Layouts[i].X += a.DeltaX
				v.Layouts[i].Y += a.DeltaY
				v.Layouts[i].Depth += a.DeltaDepth
			}
		}
	case MoveApplication:
		if !finite(float64(a.DeltaX)) || !finite(float64(a.DeltaY)) || !finite(float64(a.DeltaDepth)) {
			return fmt.Errorf("application movement must be finite")
		}
		selected := v.movementSelectionFor(a.ApplicationKey)
		if selected == 0 {
			return fmt.Errorf("unknown application layout %q", a.ApplicationKey)
		}
		for i := range v.Layouts {
			if selected&(1<<i) != 0 {
				v.Layouts[i].X += a.DeltaX
				v.Layouts[i].Y += a.DeltaY
				v.Layouts[i].Depth += a.DeltaDepth
			}
		}
	case ResizeApplication:
		i := v.index(a.ApplicationKey)
		if i < 0 || v.Layouts[i].Space != v.Space {
			return fmt.Errorf("unknown application layout %q", a.ApplicationKey)
		}
		if !finite(float64(a.DeltaX)) || !finite(float64(a.DeltaY)) {
			return fmt.Errorf("application resize movement must be finite")
		}
		if a.Width < minApplicationWidth || a.Width > maxApplicationWidth || a.Height < minApplicationHeight || a.Height > maxApplicationHeight {
			return fmt.Errorf("application size must be between %dx%d and %dx%d", minApplicationWidth, minApplicationHeight, maxApplicationWidth, maxApplicationHeight)
		}
		p := &v.Layouts[i]
		wasMaximized := p.Maximized
		p.X, p.Y = p.X+a.DeltaX, p.Y+a.DeltaY
		p.Width, p.Height = a.Width, a.Height
		p.Maximized = false
		if wasMaximized {
			// Maximized uses the Compact physical-size basis. Keep that basis
			// when direct resizing leaves maximize mode, avoiding a scale jump.
			p.Wide = false
		}
		p.RestoreWidth, p.RestoreHeight, p.RestoreWide = 0, 0, false
	case GroupApplications:
		if bits.OnesCount32(v.Selected) < 2 {
			return fmt.Errorf("select at least two applications to group")
		}
		used := uint64(0)
		for i, p := range v.Layouts {
			if v.Selected&(1<<i) == 0 {
				used |= 1 << p.Group
			}
		}
		group := uint8(1)
		for group < MaxApplicationLayouts && used&(1<<group) != 0 {
			group++
		}
		for i := range v.Layouts {
			if v.Selected&(1<<i) != 0 {
				v.Layouts[i].Group = group
			}
		}
	case UngroupApplications:
		selected := v.movementSelection()
		for i := range v.Layouts {
			if selected&(1<<i) != 0 {
				v.Layouts[i].Group = 0
			}
		}
	default:
		return fmt.Errorf("unknown application action %q", a.Kind)
	}
	v.aliases()
	return v.validate()
}
