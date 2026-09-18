//go:build linux

package app

import (
	"github.com/codemodify/worldr/internal/render"
	"testing"
)

func applicationTextureCenter(t *testing.T, frame render.Frame, texture *render.Texture) (float32, float32) {
	t.Helper()
	for _, command := range frame.Commands {
		if command.Kind != render.SceneCommand {
			continue
		}
		for _, draw := range command.Draws {
			if draw.Texture != texture {
				continue
			}
			p, m := command.View.Projection, draw.Model
			cx := p[0]*m[12] + p[4]*m[13] + p[8]*m[14] + p[12]*m[15]
			cy := p[1]*m[12] + p[5]*m[13] + p[9]*m[14] + p[13]*m[15]
			cw := p[3]*m[12] + p[7]*m[13] + p[11]*m[14] + p[15]*m[15]
			if cw <= 0 {
				t.Fatal("application is behind the camera")
			}
			vp := command.View.Viewport
			return vp[0] + (cx/cw+1)*vp[2]/2, vp[1] + (cy/cw+1)*vp[3]/2
		}
	}
	t.Fatal("frame omitted application texture")
	return 0, 0
}
