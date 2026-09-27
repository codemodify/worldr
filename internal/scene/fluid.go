package scene

import (
	"github.com/codemodify/worldr/internal/render"
	fluid "github.com/codemodify/worldr/sdk/fluid/v1"
)

// Fluid records a native procedural glass field in command order. Surfaces use
// framebuffer coordinates; their slice is borrowed until the frame is submitted.
// Text and images recorded afterward remain independent, sharp overlays.
func (c *Canvas) Fluid(field fluid.Field) {
	c.flushUI()
	c.commands = append(c.commands, render.Command{Kind: render.FluidCommand, Fluid: field})
}
