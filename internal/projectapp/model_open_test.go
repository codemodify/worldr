package projectapp

import (
	"os"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func TestModelOpenUsesAnchoredHandlerOnlyAfterExplicitActivation(t *testing.T) {
	for _, name := range []string{"mesh.obj", "part.STL", "assembly.worldr-model.json", "Part.WORLDR-MODEL.JSON"} {
		for _, activation := range []string{"enter", "button", "double-click"} {
			t.Run(name+"/"+activation, func(t *testing.T) {
				if !IsModelPath(name) {
					t.Fatal("supported model extension was not recognized")
				}
				p, reader := controlledProvider(t)
				listing := nextCall(t, reader)
				listing.answer <- result{entries: []entry{{name: name, kind: fileEntry}}}
				pollUntil(t, p, func() bool { return p.selected == 0 })
				if p.loadingFile || len(reader.calls) != 0 || !strings.Contains(p.message, "3D model") {
					t.Fatal("selection opened a model as a text preview or lacked its hint")
				}
				var opened *os.File
				p.SetOpenHandler(func(file *os.File, displayName string) error {
					if displayName != name {
						t.Fatal("model name was lost")
					}
					opened = file
					return nil
				})
				p.Focus(1)
				switch activation {
				case "enter":
					p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true})
				case "button":
					p.Send(1, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 205, Y: 70})
				case "double-click":
					for _, stamp := range []uint32{100, 200} {
						p.Send(1, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 20, Y: contentTop + 3, Time: stamp})
					}
				}
				call := nextCall(t, reader)
				if !call.request.openMedia || call.request.path != name {
					t.Fatal("explicit model open did not use the descriptor path")
				}
				file := mediaFile(t)
				call.answer <- result{file: file}
				pollUntil(t, p, func() bool { return !p.loadingFile })
				if opened != file || !strings.Contains(p.message, "model inspector") {
					t.Fatal("host callback did not receive model ownership", p.message)
				}
				p.Close()
				if _, err := file.Stat(); err != nil {
					t.Fatal("browser closed a transferred model descriptor", err)
				}
			})
		}
	}
	for _, name := range []string{"notes.json", "mesh.obj.txt", "model.worldr-model.json.bak", "stl", "mesh.obj/file"} {
		if IsModelPath(name) {
			t.Fatal("unrelated filename was recognized as a model", name)
		}
	}
}
