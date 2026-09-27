package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

// BenchmarkSkinnedDesktopFrame isolates host update/layout/scene recording from
// the GPU and child apps. Fixtures use the actual reference workspace layouts.
func BenchmarkSkinnedDesktopFrame(b *testing.B) {
	for _, preset := range []string{"advanced", "hologram", "merrick"} {
		b.Run(preset, func(b *testing.B) {
			w, err := NewDesktop()
			if err != nil {
				b.Fatal(err)
			}
			defer w.Close()
			data, err := os.ReadFile("../../examples/" + preset + "-desktop/layout.json")
			if err != nil {
				b.Fatal(err)
			}
			var envelope struct {
				State json.RawMessage `json:"state"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil {
				b.Fatal(err)
			}
			if err := w.LoadState(envelope.State); err != nil {
				b.Fatal(err)
			}
			selected, err := skin.Builtin(preset)
			if err != nil {
				b.Fatal(err)
			}
			if err := w.SetSkin(selected); err != nil {
				b.Fatal(err)
			}
			apps := &fakeApplications{}
			for i, p := range w.Document().View.Application.Layouts {
				if p.Key == "" {
					continue
				}
				texture, err := render.NewTexture(p.Width, p.Height, make([]byte, p.Width*p.Height*4))
				if err != nil {
					b.Fatal(err)
				}
				apps.surfaces = append(apps.surfaces, experience.ApplicationSurface{ID: uint64(i + 1), Key: p.Key, Title: fmt.Sprintf("Reference application %d", i+1), Texture: texture, MinWidth: 96, MinHeight: 64})
			}
			if len(apps.surfaces) == 0 {
				b.Fatal("reference layout contains no application surfaces")
			}
			w.SetApplications(apps)
			width, height := 1440, 1180
			if preset == "hologram" {
				width, height = 1584, 1248
			}
			if preset == "merrick" {
				width, height = 2048, 896
			}
			for range 3 {
				w.Update(time.Second / 60)
				w.Draw(width, height)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w.Update(time.Second / 60)
				w.Draw(width, height)
			}
		})
	}
}
