package shell

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/codemodify/worldr/internal/engine"
)

func writeThemePNG(t *testing.T, root, theme, name string, r, g, b byte) {
	t.Helper()
	p := filepath.Join(root, "icons", theme, "16x16", "apps", name+".png")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: r, G: g, B: b, A: 255})
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
}

func TestLookupDesktopIcon(t *testing.T) {
	items := []LaunchItem{
		{ID: "org.kde.kate", Bin: "/usr/bin/kate", Icon: "kate"},
		{ID: "foot", Bin: "foot", Icon: "utilities-terminal"},
	}
	if got := LookupDesktopIcon(items, "org.kde.kate"); got != "kate" {
		t.Fatal(got)
	}
	if got := LookupDesktopIcon(items, "foot"); got != "utilities-terminal" {
		t.Fatal(got)
	}
	if got := LookupDesktopIcon(items, "unknown.app"); got != "unknown.app" {
		t.Fatal(got)
	}
	if LookupDesktopIcon(items, "") != "" {
		t.Fatal("empty")
	}
}

func TestFillThemeIconsClientWins(t *testing.T) {
	root := t.TempDir()
	writeThemePNG(t, root, "hicolor", "foot", 0, 255, 0)
	t.Setenv("XDG_DATA_HOME", root)
	t.Setenv("XDG_DATA_DIRS", root)
	t.Setenv("XDG_ICON_THEME", "hicolor")
	client := &engine.Actor{AppID: "foot", IconName: "foot", IconPix: []byte{9, 8, 7, 255}, IconW: 1, IconH: 1, IconStride: 4}
	need := &engine.Actor{AppID: "foot", IconName: "foot"}
	miss := &engine.Actor{AppID: "no-such-worldr-icon"}
	FillThemeIcons([]*engine.Actor{client, need, miss}, nil)
	if client.IconPix[0] != 9 {
		t.Fatal("xdg_toplevel_icon buffer wins")
	}
	if !need.HasIcon() || need.IconPix[1] != 255 {
		t.Fatalf("theme fill: %+v", need)
	}
	if miss.HasIcon() {
		t.Fatal("missing stays glyph")
	}
}

func TestLoadCatalogResolvesIcon(t *testing.T) {
	root := t.TempDir()
	apps := filepath.Join(root, "applications")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDesktop(t, apps, "ok.desktop", `[Desktop Entry]
Type=Application
Name=Ok App
Exec=ok-bin
Icon=foot
`)
	writeThemePNG(t, root, "hicolor", "foot", 0, 0, 255)
	t.Setenv("XDG_DATA_HOME", root)
	t.Setenv("XDG_DATA_DIRS", root)
	t.Setenv("XDG_ICON_THEME", "hicolor")
	got := LoadCatalog([]string{apps}, "", false)
	if len(got) != 1 || got[0].Icon != "foot" || !got[0].hasThemeIcon() {
		t.Fatalf("%+v", got)
	}
}

func TestDrawLauncherPaintsThemeIcon(t *testing.T) {
	const w, h, stride = 200, 200, 800
	dst := make([]byte, stride*h)
	red := []byte{0, 0, 255, 255}
	drawLauncher(dst, stride, w, h, PanelH, LauncherDraw{
		Items:  []string{"foot"},
		Icons:  []LaunchIcon{{Pix: red, W: 1, H: 1, Stride: 4}},
		Select: 0,
	})
	// default glyph is teal-ish; a client/theme icon is scaled red → B high
	found := false
	for i := 0; i+4 <= len(dst); i += 4 {
		if dst[i+2] > 80 && dst[i+0] < 40 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected red theme icon in launcher row")
	}
}

func (it LaunchItem) hasThemeIcon() bool {
	return len(it.IconPix) > 0 && it.IconW > 0 && it.IconH > 0
}
