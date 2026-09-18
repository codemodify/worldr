package app

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestApplicationProfilePreservesArgumentsAndIdentities(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps.json")
	data := `{"version":1,"applications":[{"id":"terminal","command":"foot","args":["--config=/dev/null","sh","-c","printf '%s' '$literal'"]},{"id":"browser","command":"chromium","args":["--ozone-platform=wayland","https://example.invalid/a b"]}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	o, err := Parse([]string{"--apps", path}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	launches, err := applicationLaunches(o)
	if err != nil {
		t.Fatal(err)
	}
	want := []ApplicationLaunch{{ID: "terminal", Command: "foot", Args: []string{"--config=/dev/null", "sh", "-c", "printf '%s' '$literal'"}}, {ID: "browser", Command: "chromium", Args: []string{"--ozone-platform=wayland", "https://example.invalid/a b"}}}
	if !reflect.DeepEqual(launches, want) {
		t.Fatalf("profile changed argument boundaries: %+v", launches)
	}
	if _, err := Parse([]string{"--apps", path, "--app=foot"}, io.Discard); err == nil {
		t.Fatal("mixed launch configuration accepted")
	}
}

func TestApplicationProfileRejectsAmbiguousOrInvalidLaunches(t *testing.T) {
	for _, data := range []string{
		`{"version":2,"applications":[{"id":"a","command":"foot"}]}`,
		`{"version":1,"applications":[]}`,
		`{"version":1,"applications":[{"id":"a","command":"foot"},{"id":"a","command":"foot"}]}`,
		`{"version":1,"applications":[{"id":"a/b","command":"foot"}]}`,
		`{"version":1,"applications":[{"id":"a","command":" "}]}`,
		`{"version":1,"applications":[{"id":"a","command":"foot","args":["\u0000"]}]}`,
		`{"version":1,"applications":[{"id":"a","command":"foot","unknown":true}]}`,
		`{"version":1,"applications":[{"id":"a","command":"foot"}]} {}`,
	} {
		t.Run(data, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "apps.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadApplicationProfile(path); err == nil {
				t.Fatal("invalid profile accepted")
			}
		})
	}
}
