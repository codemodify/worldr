package shell

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDesktopExecStripsFieldCodes(t *testing.T) {
	got := parseDesktopExec(`foot %F`)
	if len(got) != 1 || got[0] != "foot" {
		t.Fatalf("%q", got)
	}
	got = parseDesktopExec(`/usr/bin/app --file %f --url %u`)
	if len(got) != 3 || got[0] != "/usr/bin/app" || got[1] != "--file" || got[2] != "--url" {
		t.Fatalf("%q", got)
	}
	got = parseDesktopExec(`env "FOO=bar baz" myapp %%done`)
	if len(got) != 4 || got[0] != "env" || got[1] != "FOO=bar baz" || got[2] != "myapp" || got[3] != "%done" {
		t.Fatalf("%q", got)
	}
}

func TestLoadCatalogFiltersAndFallback(t *testing.T) {
	dir := t.TempDir()
	writeDesktop(t, dir, "ok.desktop", `[Desktop Entry]
Type=Application
Name=Ok App
Exec=ok-bin
`)
	writeDesktop(t, dir, "hidden.desktop", `[Desktop Entry]
Type=Application
Name=Hidden
Exec=hidden-bin
Hidden=true
`)
	writeDesktop(t, dir, "nodisp.desktop", `[Desktop Entry]
Type=Application
Name=No Display
Exec=nodisp-bin
NoDisplay=true
`)
	writeDesktop(t, dir, "term.desktop", `[Desktop Entry]
Type=Application
Name=Htop
Exec=htop
Terminal=true
`)
	writeDesktop(t, dir, "link.desktop", `[Desktop Entry]
Type=Link
Name=Web
Exec=https://example.com
`)
	writeDesktop(t, dir, "xeyes.desktop", `[Desktop Entry]
Type=Application
Name=XEyes
Exec=xeyes
`)
	writeDesktop(t, dir, "onlykde.desktop", `[Desktop Entry]
Type=Application
Name=KDE Only
Exec=kde-bin
OnlyShowIn=KDE;
`)
	writeDesktop(t, dir, "notgnome.desktop", `[Desktop Entry]
Type=Application
Name=Not GNOME
Exec=nog-bin
NotShowIn=GNOME;
`)

	got := LoadCatalog([]string{dir}, "", false)
	if names := labelsOf(got); len(names) != 3 || names[0] != "KDE Only" || names[1] != "Not GNOME" || names[2] != "Ok App" {
		t.Fatalf("empty XDG_CURRENT_DESKTOP keeps OnlyShowIn/NotShowIn: %v", names)
	}

	got = LoadCatalog([]string{dir}, "", true)
	if names := labelsOf(got); len(names) != 4 || names[3] != "XEyes" || !got[3].X11 {
		t.Fatalf("xwayland should keep xeyes: %v", names)
	}

	got = LoadCatalog([]string{dir}, "GNOME", false)
	if names := labelsOf(got); len(names) != 1 || names[0] != "Ok App" {
		t.Fatalf("GNOME should drop OnlyShowIn=KDE and NotShowIn=GNOME: %v", names)
	}

	got = LoadCatalog([]string{dir}, "KDE", false)
	if names := labelsOf(got); len(names) != 3 || names[0] != "KDE Only" || names[1] != "Not GNOME" || names[2] != "Ok App" {
		t.Fatalf("KDE: %v", names)
	}

	empty := t.TempDir()
	got = LoadCatalog([]string{empty}, "", false)
	if len(got) != 2 || got[0].Bin != "foot" || got[1].Bin != "weston-simple-shm" {
		t.Fatalf("fallback %+v", got)
	}
	got = LoadCatalog([]string{empty}, "", true)
	if len(got) < 3 || got[2].Bin != "xeyes" || !got[2].X11 {
		t.Fatalf("x11 fallback %+v", got)
	}
}

func TestLoadCatalogFirstDirWins(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	writeDesktop(t, a, "same.desktop", `[Desktop Entry]
Type=Application
Name=Local
Exec=local-bin
`)
	writeDesktop(t, b, "same.desktop", `[Desktop Entry]
Type=Application
Name=System
Exec=system-bin
`)
	got := LoadCatalog([]string{a, b}, "", false)
	if len(got) != 1 || got[0].Label != "Local" || got[0].Bin != "local-bin" {
		t.Fatalf("%+v", got)
	}
}

func TestLoadCatalogTryExecMissingDropped(t *testing.T) {
	dir := t.TempDir()
	writeDesktop(t, dir, "try.desktop", `[Desktop Entry]
Type=Application
Name=Missing Try
Exec=whatever
TryExec=/no/such/worldr-tryexec-bin
`)
	got := LoadCatalog([]string{dir}, "", false)
	if len(got) != 2 || got[0].Bin != "foot" {
		t.Fatalf("TryExec miss should fall through to fallback if nothing else: %+v", got)
	}
}

func TestFallbackCatalog(t *testing.T) {
	c := fallbackCatalog(false)
	if len(c) != 2 || c[0].Bin != "foot" || c[1].X11 {
		t.Fatalf("%+v", c)
	}
	c = fallbackCatalog(true)
	if len(c) < 3 || c[2].Bin != "xeyes" || !c[2].X11 {
		t.Fatalf("x11 %+v", c)
	}
}

func writeDesktop(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func labelsOf(items []LaunchItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}
