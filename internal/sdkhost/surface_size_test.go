package sdkhost

import (
	"testing"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
)

func TestSDKSurfaceCarriesOptionalResizeMinimumIntoWorkspace(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {96, 64}} {
		p := bareProvider()
		snapshot := nativeapp.Snapshot{
			Textures: []nativeapp.TextureUpdate{testTexture(1, 1, 4, 4, 0)},
			Surfaces: []nativeapp.Surface{{ID: 1, Key: "main", Title: "Small tool", Texture: 1, MinWidth: size[0], MinHeight: size[1]}},
		}
		if err := p.apply(snapshot); err != nil {
			t.Fatal(err)
		}
		if got := p.Surfaces()[0]; got.MinWidth != size[0] || got.MinHeight != size[1] {
			t.Fatalf("host lost per-surface resize limits: %+v", got)
		}
	}
}
