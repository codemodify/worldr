package projectapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func TestDatasetOpenUsesExplicitNativeResearchTypes(t *testing.T) {
	for _, name := range []string{"experiment.csv", "signals.TSV", "run.worldr-data.json", "RUN.WORLDR-DATA.JSON"} {
		if !IsDatasetPath(name) || !mediaPath(name) {
			t.Fatal("dataset was not routed to the research workbench", name)
		}
		p, reader := controlledProvider(t)
		listing := nextCall(t, reader)
		listing.answer <- result{request: listing.request, entries: []entry{{name: name, kind: fileEntry}}}
		pollUntil(t, p, func() bool { return !p.loadingDirectory })
		var opened string
		p.SetOpenHandler(func(file *os.File, label string) error {
			opened = label
			return file.Close()
		})
		p.Focus(1)
		p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true})
		call := nextCall(t, reader)
		if !call.request.openMedia {
			t.Fatal("dataset did not request an anchored descriptor")
		}
		path := filepath.Join(t.TempDir(), "dataset")
		if err := os.WriteFile(path, []byte("x,y\n1,2\n"), 0600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		call.answer <- result{request: call.request, file: file}
		pollUntil(t, p, func() bool { return !p.loadingFile })
		if opened != name || !strings.Contains(p.message, "research workbench") {
			t.Fatal("dataset did not reach research viewer", opened, p.message)
		}
		p.Close()
	}
	for _, name := range []string{"settings.json", "data.csv.bak", "worldr-data.json", "table.txt"} {
		if IsDatasetPath(name) {
			t.Fatal("ordinary document was claimed as a research dataset", name)
		}
	}
}
