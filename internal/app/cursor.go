package app

import (
	"math"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
)

// The experience owns all capacity in its borrowed slices. Reusable host
// storage prevents appending a cursor from overwriting an experience's data.
type cursorOverlay struct {
	vertices []render.Vertex
	commands []render.Command
}

func (c *cursorOverlay) appendFor(f render.Frame, x, y float32, atlas render.Atlas, owner any) render.Frame {
	if provider, ok := owner.(experience.PointerCursor); ok {
		if cursor, set := provider.Cursor(); set {
			if cursor.Hidden {
				return f
			}
			if cursor.Texture != nil && cursor.Scale > 0 {
				width, height := cursor.Texture.Size()
				bounds := [4]float32{x - cursor.HotspotX, y - cursor.HotspotY, float32(width) / cursor.Scale, float32(height) / cursor.Scale}
				if cursor.LogicalSize != ([2]float32{}) {
					bounds[2], bounds[3] = cursor.LogicalSize[0], cursor.LogicalSize[1]
				}
				valid := bounds[2] > 0 && bounds[3] > 0
				for _, value := range [...]float32{cursor.Scale, bounds[0], bounds[1], bounds[2], bounds[3], bounds[0] + bounds[2], bounds[1] + bounds[3]} {
					valid = valid && !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
				}
				if valid {
					c.copyCommands(f.Commands)
					c.commands = append(c.commands, render.Command{Kind: render.ImageCommand, Image: render.Image{Texture: cursor.Texture, Bounds: bounds}})
					f.Commands = c.commands
					return f
				}
			}
		}
	}
	return c.append(f, x, y, atlas)
}

func (c *cursorOverlay) copyCommands(commands []render.Command) {
	previous := len(c.commands)
	c.commands = append(c.commands[:0], commands...)
	if previous > len(c.commands) {
		clear(c.commands[len(c.commands):previous])
	}
}

func (c *cursorOverlay) append(f render.Frame, x, y float32, atlas render.Atlas) render.Frame {
	c.vertices = append(c.vertices[:0], f.Vertices...)
	c.copyCommands(f.Commands)
	f.Vertices, f.Commands = c.vertices, c.commands
	first := len(f.Vertices)
	u, t := .5/float32(atlas.Width), .5/float32(atlas.Height)
	for _, p := range [][2]float32{{0, 0}, {0, 20}, {5, 14}, {5, 14}, {15, 14}, {0, 0}} {
		f.Vertices = append(f.Vertices, render.Vertex{X: x + p[0], Y: y + p[1], U: u, V: t, R: .94, G: .96, B: .96, A: 1})
	}
	f.Commands = append(f.Commands, render.Command{Kind: render.OverlayCommand, First: first, Count: len(f.Vertices) - first})
	c.vertices, c.commands = f.Vertices, f.Commands
	return f
}
