package icontheme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeIndex(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInheritChainFromIndexTheme(t *testing.T) {
	root := t.TempDir()
	writeIndex(t, filepath.Join(root, "icons/breeze/index.theme"), `[Icon Theme]
Name=Breeze
Inherits=breeze-dark,hicolor
`)
	writeIndex(t, filepath.Join(root, "icons/breeze-dark/index.theme"), `[Icon Theme]
Name=Breeze Dark
Inherits=hicolor
`)
	got := InheritChain("breeze", []string{root})
	if len(got) < 3 || got[0] != "breeze" || got[1] != "breeze-dark" || got[len(got)-1] != "hicolor" {
		t.Fatalf("%v", got)
	}
}

func TestInheritChainCycleAndMissingIndex(t *testing.T) {
	root := t.TempDir()
	writeIndex(t, filepath.Join(root, "icons/a/index.theme"), "[Icon Theme]\nInherits=b\n")
	writeIndex(t, filepath.Join(root, "icons/b/index.theme"), "[Icon Theme]\nInherits=a,hicolor\n")
	got := InheritChain("a", []string{root})
	seen := map[string]int{}
	for _, th := range got {
		seen[th]++
		if seen[th] > 1 {
			t.Fatalf("dup %q in %v", th, got)
		}
	}
	if seen["a"] != 1 || seen["b"] != 1 || seen["hicolor"] != 1 {
		t.Fatalf("%v", got)
	}
	got = InheritChain("nope", []string{root})
	if len(got) != 2 || got[0] != "nope" || got[1] != "hicolor" {
		t.Fatalf("missing index: %v", got)
	}
}

func TestResolveWalksInherits(t *testing.T) {
	root := t.TempDir()
	writeIndex(t, filepath.Join(root, "icons/breeze/index.theme"), "[Icon Theme]\nInherits=legacy\n")
	writePNG(t, filepath.Join(root, "icons/legacy/24x24/apps/only-legacy.png"), 0, 9, 0, 255)
	p, ok := Resolve("only-legacy", Search{Theme: "breeze", Dirs: []string{root}, Want: 24})
	if !ok || !strings.Contains(p, "legacy") {
		t.Fatalf("inherit lookup %q %v", p, ok)
	}
}

func TestParseInheritsIgnoresOtherSections(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "icons/x/index.theme")
	writeIndex(t, p, `[Icon Theme]
Name=X
Inherits=parent

[16x16/apps]
Size=16
Inherits=not-a-theme
`)
	got := parseInherits(p)
	if len(got) != 1 || got[0] != "parent" {
		t.Fatalf("%v", got)
	}
}
