package nativeui_test

import (
	"fmt"
	"image"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
)

func ExamplePainter() {
	theme, _ := nativeui.Builtin(nativeui.Telemetry, nativeui.Notched)
	painter, _ := nativeui.NewPainter(theme)
	framebuffer := image.NewRGBA(image.Rect(0, 0, 320, 120))
	button := nativeui.Control{ID: "scan", Kind: nativeui.KindButton, Bounds: image.Rect(20, 20, 160, 54), Label: "RUN SCAN", Icon: nativeui.IconPlay}
	_ = painter.DrawButton(framebuffer, button)

	var controls nativeui.Controller
	_ = controls.SetControls([]nativeui.Control{button})
	action := controls.Handle(nativeapp.Event{Kind: nativeapp.PointerDown, Button: nativeapp.ButtonPrimary, X: 30, Y: 30})
	action = controls.Handle(nativeapp.Event{Kind: nativeapp.PointerUp, Button: nativeapp.ButtonPrimary, X: 30, Y: 30})
	fmt.Println(action.ID, action.Activated, len(controls.Semantics().Nodes))
	// Output: scan true 1
}
