package nativeui

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"unicode/utf8"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
)

// Kind identifies control behavior. Several visual variants intentionally map
// to the same nativeapp semantic role because the v1 wire contract is unchanged.
type Kind uint8

const (
	KindLabel Kind = iota
	KindIcon
	KindButton
	KindIconButton
	KindField
	KindTextArea
	KindSwitch
	KindCheckbox
	KindRadio
	KindSlider
	KindProgress
	KindMeter
	KindTab
	KindSegment
	KindMenuItem
	KindListRow
	KindTreeRow
	KindTableRow
	KindScrollbar
	KindSplitter
	KindPanel
	KindCard
	KindToolbar
	KindDialog
	KindPopover
	KindTooltip
	KindBadge
	KindSeparator
)

// State contains visual and interaction state shared by every control.
type State struct {
	Hovered  bool
	Pressed  bool
	Focused  bool
	Selected bool
	Disabled bool
	Invalid  bool
}

// Icon names portable vector glyphs supplied by Painter.
type Icon string

const (
	IconNone         Icon = ""
	IconAdd          Icon = "add"
	IconRemove       Icon = "remove"
	IconClose        Icon = "close"
	IconCheck        Icon = "check"
	IconPlay         Icon = "play"
	IconPause        Icon = "pause"
	IconStop         Icon = "stop"
	IconBack         Icon = "back"
	IconForward      Icon = "forward"
	IconSearch       Icon = "search"
	IconSettings     Icon = "settings"
	IconChevronRight Icon = "chevron-right"
)

// Control is the common description accepted by Painter and Controller. Min,
// Max, Value, Step and Page apply to sliders, scrollbars and splitters. Text is
// field content; Placeholder is used only when Text is empty.
type Control struct {
	ID     string
	Kind   Kind
	Bounds image.Rectangle
	// RangeBounds is the available parent span for a splitter drag. Zero uses
	// the splitter's long side. The visible Bounds remains its pointer target.
	RangeBounds image.Rectangle
	Label       string
	Description string
	Text        string
	Placeholder string
	ValueText   string
	Icon        Icon
	State       State
	Min         float64
	Max         float64
	Value       float64
	Step        float64
	Page        float64
	Indent      int
}

func finite(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func validText(values ...string) bool {
	for _, value := range values {
		if len(value) > 65536 || !utf8.ValidString(value) {
			return false
		}
	}
	return true
}

func (c Control) validate(requireID bool) error {
	if requireID && (c.ID == "" || len(c.ID) > 128 || !utf8.ValidString(c.ID)) {
		return fmt.Errorf("native UI: control ID must contain 1..128 UTF-8 bytes")
	}
	if !validText(c.Label, c.Description, c.Text, c.Placeholder, c.ValueText) {
		return fmt.Errorf("native UI: control text is invalid or exceeds 65536 bytes")
	}
	if c.Bounds.Dx() < 0 || c.Bounds.Dy() < 0 || c.Bounds.Dx() > 16384 || c.Bounds.Dy() > 16384 {
		return fmt.Errorf("native UI: control bounds are outside supported limits")
	}
	if c.RangeBounds != (image.Rectangle{}) && (c.RangeBounds.Empty() || c.RangeBounds.Dx() > 16384 || c.RangeBounds.Dy() > 16384) {
		return fmt.Errorf("native UI: invalid splitter range bounds")
	}
	if requireID {
		if c.Bounds.Empty() || c.Bounds.Min.X < 0 || c.Bounds.Min.Y < 0 || c.Bounds.Max.X > nativeapp.MaxTextureWidth || c.Bounds.Max.Y > nativeapp.MaxTextureHeight {
			return fmt.Errorf("native UI: semantic bounds must fit the maximum v1 surface")
		}
		if len(c.Label) > 512 || len(c.ValueText) > 4096 || len(c.Description) > 1024 || (c.Kind == KindField || c.Kind == KindTextArea) && len(c.Text) > 4096 {
			return fmt.Errorf("native UI: semantic text exceeds native-app v1 limits")
		}
	}
	if c.Indent < 0 || c.Indent > 64 {
		return fmt.Errorf("native UI: row indentation must be within 0..64")
	}
	if !finite(c.Min, c.Max, c.Value, c.Step, c.Page) {
		return fmt.Errorf("native UI: control range contains a non-finite value")
	}
	if c.hasRange() {
		if c.Max <= c.Min || c.Value < c.Min || c.Value > c.Max || c.Step < 0 || c.Page < 0 {
			return fmt.Errorf("native UI: invalid control range")
		}
	}
	return nil
}

func (c Control) hasRange() bool {
	return c.rangeControl() || c.Kind == KindProgress || c.Kind == KindMeter
}

func (c Control) rangeControl() bool {
	return c.Kind == KindSlider || c.Kind == KindScrollbar || c.Kind == KindSplitter
}

func (c Control) interactive() bool {
	switch c.Kind {
	case KindButton, KindIconButton, KindField, KindTextArea, KindSwitch, KindCheckbox, KindRadio, KindSlider, KindTab, KindSegment, KindMenuItem, KindListRow, KindTreeRow, KindTableRow, KindScrollbar, KindSplitter:
		return !c.State.Disabled && !c.Bounds.Empty()
	}
	return false
}

func (c Control) role() nativeapp.Role {
	switch c.Kind {
	case KindField, KindTextArea:
		return nativeapp.RoleTextField
	case KindSlider, KindScrollbar, KindSplitter:
		return nativeapp.RoleSlider
	case KindMenuItem, KindListRow, KindTreeRow, KindTableRow:
		return nativeapp.RoleMenuItem
	case KindProgress, KindMeter, KindBadge:
		return nativeapp.RoleStatus
	case KindIcon:
		return nativeapp.RoleImage
	case KindLabel, KindPanel, KindCard, KindToolbar, KindDialog, KindPopover, KindTooltip, KindSeparator:
		return nativeapp.RoleLabel
	default:
		return nativeapp.RoleButton
	}
}

// SemanticNode converts a control to the unchanged native-app v1 semantic
// vocabulary. Toggle-like controls use Selected, and ranges use ValueText.
func (c Control) SemanticNode() nativeapp.SemanticNode {
	value := c.ValueText
	if value == "" && c.hasRange() {
		value = strconv.FormatFloat(c.Value, 'f', -1, 64)
	}
	if value == "" && (c.Kind == KindField || c.Kind == KindTextArea) {
		value = c.Text
	}
	return nativeapp.SemanticNode{
		ID: c.ID, Role: c.role(), Label: c.Label, Value: value, Description: c.Description,
		Bounds:   nativeapp.Rect{X: c.Bounds.Min.X, Y: c.Bounds.Min.Y, Width: c.Bounds.Dx(), Height: c.Bounds.Dy()},
		Disabled: c.State.Disabled, Selected: c.State.Selected,
	}
}
