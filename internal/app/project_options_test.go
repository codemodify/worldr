package app

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestExperienceOptionsKeepDesktopDefaultAndExplicitChoice(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"default desktop", nil, "workspace"},
		{"demo study", []string{"--demo"}, "axial"},
		{"explicit study", []string{"--experience=axial"}, "axial"},
		{"explicit study demo", []string{"--experience=axial", "--demo"}, "axial"},
	} {
		t.Run(test.name, func(t *testing.T) {
			o, err := Parse(test.args, io.Discard)
			if err != nil || o.Experience != test.want {
				t.Fatalf("Parse(%q): experience=%q, err=%v; want %q", test.args, o.Experience, err, test.want)
			}
		})
	}
	for _, value := range []string{"", "unknown", "AXIAL", "workspace,axial"} {
		if _, err := Parse([]string{"--experience=" + value}, io.Discard); err == nil {
			t.Fatalf("accepted unknown experience %q", value)
		}
	}
	for _, args := range [][]string{{"--experience=workspace", "--demo"}, {"--demo", "--experience=workspace"}} {
		if _, err := Parse(args, io.Discard); err == nil {
			t.Fatalf("accepted unsupported desktop demo: %q", args)
		}
	}
}

func TestProjectOptionIsSeparateFromLegacyApplicationArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project with spaces λ")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	o, err := Parse([]string{"--project=" + path, "--app=foot", "--", "--title=legacy", "sh"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if o.Project != path || o.Experience != "workspace" || o.Application != "foot" || len(o.ApplicationArgs) != 2 || o.ApplicationArgs[0] != "--title=legacy" || o.ApplicationArgs[1] != "sh" {
		t.Fatalf("project path or legacy launch arguments changed: %+v", o)
	}
}

func TestHostedAxialOptionKeepsGeneralWorkspace(t *testing.T) {
	o, err := Parse([]string{"--axial"}, io.Discard)
	if err != nil || !o.Axial || o.Experience != "workspace" {
		t.Fatalf("hosted AXIAL option: %+v err=%v", o, err)
	}
}
