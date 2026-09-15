package session

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFlags(t *testing.T) {
	o, err := ParseFlags([]string{"-login", "-desktop=worldr", "-user=alice", "--", "--backend=nested", "-duration=1s"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.Login || o.User != "alice" || o.Desktop != "worldr" {
		t.Fatalf("%+v", o)
	}
	if len(o.ShellArgs) != 2 || o.ShellArgs[0] != "--backend=nested" {
		t.Fatalf("shell args %v", o.ShellArgs)
	}
	o, err = ParseFlags([]string{})
	if err != nil || o.Desktop != DefaultDesktop || o.Login {
		t.Fatalf("default %+v %v", o, err)
	}
}

func TestCheckLogin(t *testing.T) {
	if err := CheckLogin("alice", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := CheckLogin("bob", "alice"); err == nil || !strings.Contains(err.Error(), "not the current user") {
		t.Fatalf("want reject: %v", err)
	}
	if err := CheckLogin("", "alice"); err == nil {
		t.Fatal("empty")
	}
	if err := CheckLogin("alice", ""); err == nil {
		t.Fatal("no current")
	}
}

func TestResolveUserPromptAndAuto(t *testing.T) {
	var errOut bytes.Buffer
	got, err := ResolveUser(Options{Login: true}, "alice", strings.NewReader("alice\n"), &errOut)
	if err != nil || got != "alice" {
		t.Fatalf("%q %v", got, err)
	}
	if !strings.Contains(errOut.String(), "login:") {
		t.Fatal(errOut.String())
	}
	if _, err := ResolveUser(Options{Login: true}, "alice", strings.NewReader("bob\n"), ioDiscard()); err == nil {
		t.Fatal("wrong name")
	}
	got, err = ResolveUser(Options{User: "alice"}, "alice", nil, nil)
	if err != nil || got != "alice" {
		t.Fatalf("auto %q %v", got, err)
	}
	if _, err := ResolveUser(Options{User: "root"}, "alice", nil, nil); err == nil {
		t.Fatal("user switch")
	}
	got, err = ResolveUser(Options{}, "alice", nil, nil)
	if err != nil || got != "alice" {
		t.Fatalf("passthrough %q %v", got, err)
	}
}

type discard struct{}

func ioDiscard() *discard                    { return &discard{} }
func (*discard) Write(p []byte) (int, error) { return len(p), nil }

func TestPrepareEnvAndMerge(t *testing.T) {
	dir := t.TempDir()
	env, err := PrepareEnv(Options{RuntimeDir: dir, Desktop: "worldr"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if env.RuntimeDir != dir || env.SessionType != "wayland" || env.Desktop != "worldr" {
		t.Fatalf("%+v", env)
	}
	ov := env.Overlay()
	joined := strings.Join(ov, "\n")
	for _, want := range []string{
		"XDG_RUNTIME_DIR=" + dir,
		"XDG_SESSION_TYPE=wayland",
		"XDG_CURRENT_DESKTOP=worldr",
		"XDG_SESSION_DESKTOP=worldr",
		"XDG_SESSION_CLASS=user",
		"DESKTOP_SESSION=worldr",
		"USER=alice",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
	merged := MergeEnviron([]string{"FOO=1", "XDG_SESSION_TYPE=tty", "USER=old"}, ov)
	var typ, user string
	hasFoo := false
	for _, kv := range merged {
		switch {
		case kv == "FOO=1":
			hasFoo = true
		case strings.HasPrefix(kv, "XDG_SESSION_TYPE="):
			typ = kv
		case strings.HasPrefix(kv, "USER="):
			user = kv
		}
	}
	if !hasFoo || typ != "XDG_SESSION_TYPE=wayland" || user != "USER=alice" {
		t.Fatalf("merge %v", merged)
	}
}

func TestFindShellSiblingAndExplicit(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "worldr-shell")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindShell("", filepath.Join(dir, "worldr-session"))
	if err != nil || got != bin {
		// Abs may resolve differently
		if err != nil || filepath.Base(got) != "worldr-shell" {
			t.Fatalf("sibling %q %v", got, err)
		}
	}
	got, err = FindShell(bin, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "worldr-shell" {
		t.Fatal(got)
	}
	if _, err := FindShell(dir, ""); err == nil {
		t.Fatal("dir")
	}
}

func TestRunPrintEnvAndDryRun(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "worldr-shell")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho SHELL_RAN\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := Run(&out, ioDiscard(), nil, filepath.Join(dir, "worldr-session"), Options{
		PrintEnv:   true,
		RuntimeDir: dir,
		Desktop:    "worldr",
		User:       CurrentUsername(),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "XDG_SESSION_TYPE=wayland") || strings.Contains(s, "SHELL_RAN") {
		t.Fatal(s)
	}
	out.Reset()
	err = Run(&out, ioDiscard(), nil, filepath.Join(dir, "worldr-session"), Options{
		DryRun:     true,
		RuntimeDir: dir,
		ShellArgs:  []string{"--backend=headless"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s = out.String()
	if !strings.Contains(s, "shell:") || !strings.Contains(s, "--backend=headless") || strings.Contains(s, "SHELL_RAN") {
		t.Fatal(s)
	}
}

func TestRunStartsShell(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	bin := filepath.Join(dir, "worldr-shell")
	script := "#!/bin/sh\necho \"$XDG_SESSION_TYPE\" > \"$1\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := Run(&out, ioDiscard(), nil, filepath.Join(dir, "worldr-session"), Options{
		Shell:      bin,
		RuntimeDir: dir,
		ShellArgs:  []string{marker},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != "wayland" {
		t.Fatalf("marker %q", b)
	}
}

func TestDesktopFile(t *testing.T) {
	p := filepath.Join("..", "..", "contrib", "wayland-sessions", "worldr.desktop")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"[Desktop Entry]", "Exec=worldr-session", "DesktopNames=worldr", "Type=Application"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
}
