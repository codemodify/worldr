package workspace

import (
	"fmt"
	"math/bits"
	"unicode"
	"unicode/utf8"
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
		if !finite(float64(p.X)) || !finite(float64(p.Y)) || !finite(float64(p.Depth)) || abs(p.X) > 100 || abs(p.Y) > 100 || abs(p.Depth) > 40 || p.Group > MaxApplicationLayouts {
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
		}
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
