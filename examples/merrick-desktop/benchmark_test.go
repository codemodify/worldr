package main

import (
	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	"testing"
	"time"
)

type benchmarkApplication interface {
	Start(nativeapp.Host) error
	Update(time.Duration) error
	Handle(nativeapp.SurfaceID, nativeapp.Event) error
	Snapshot() nativeapp.Snapshot
	Close() error
}

var benchmarkResult nativeapp.Snapshot

func benchmarkPoint(b *testing.B, app benchmarkApplication, surface nativeapp.SurfaceID, id string) (float32, float32) {
	b.Helper()
	for _, s := range app.Snapshot().Surfaces {
		if s.ID != surface {
			continue
		}
		for _, node := range s.Semantics.Nodes {
			if node.ID == id {
				return float32(node.Bounds.X + node.Bounds.Width/2), float32(node.Bounds.Y + node.Bounds.Height/2)
			}
		}
	}
	b.Fatalf("missing benchmark control %q", id)
	return 0, 0
}
func BenchmarkDesktop(b *testing.B) {
	for _, name := range []string{"Idle", "StationaryHover", "Hover", "Selection", "Slider"} {
		b.Run(name, func(b *testing.B) {
			app := &desktop{}
			if err := app.Start(nativeapp.Host{Version: nativeapp.Version, MaxSurfaceWidth: 1920, MaxSurfaceHeight: 1080, MaxSurfaces: 8}); err != nil {
				b.Fatal(err)
			}
			defer app.Close()
			x1, y1 := benchmarkPoint(b, app, 2, "record-tab-0")
			x2, y2 := benchmarkPoint(b, app, 2, "record-tab-1")
			event := nativeapp.Event{Kind: nativeapp.PointerMove, X: x1, Y: y1}
			if err := app.Handle(2, event); err != nil {
				b.Fatal(err)
			}
			if name == "Slider" {
				x1, y1 = benchmarkPoint(b, app, 2, "nutrient-0")
				event = nativeapp.Event{Kind: nativeapp.PointerDown, Button: nativeapp.ButtonPrimary, X: x1, Y: y1}
				if err := app.Handle(2, event); err != nil {
					b.Fatal(err)
				}
				event.Kind = nativeapp.PointerMove
			}
			benchmarkResult = app.Snapshot()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				switch name {
				case "Idle":
					if err := app.Update(time.Second / 60); err != nil {
						b.Fatal(err)
					}
				case "StationaryHover":
					if err := app.Handle(2, event); err != nil {
						b.Fatal(err)
					}
				case "Hover":
					event.X, event.Y = x1, y1
					if i%2 == 0 {
						event.X, event.Y = x2, y2
					}
					if err := app.Handle(2, event); err != nil {
						b.Fatal(err)
					}
				case "Selection":
					event.X, event.Y = x1, y1
					if i%2 == 0 {
						event.X, event.Y = x2, y2
					}
					event.Kind, event.Button = nativeapp.PointerDown, nativeapp.ButtonPrimary
					if err := app.Handle(2, event); err != nil {
						b.Fatal(err)
					}
					benchmarkResult = app.Snapshot()
					event.Kind = nativeapp.PointerUp
					if err := app.Handle(2, event); err != nil {
						b.Fatal(err)
					}
				case "Slider":
					event.X = x1 - 30
					if i%2 == 0 {
						event.X = x1 + 30
					}
					if err := app.Handle(2, event); err != nil {
						b.Fatal(err)
					}
				}
				benchmarkResult = app.Snapshot()
			}
		})
	}
}
