package icontheme

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// InheritChain is the freedesktop lookup order: current theme, then each
// Inherits= name from index.theme (recursively), then hicolor.
// Cycles are skipped. Theme "" is treated as hicolor.
func InheritChain(theme string, dirs []string) []string {
	theme = strings.TrimSpace(theme)
	if theme == "" {
		theme = "hicolor"
	}
	seen := map[string]bool{}
	var out []string
	var walk func(string)
	walk = func(th string) {
		th = strings.TrimSpace(th)
		if th == "" {
			return
		}
		key := strings.ToLower(th)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, th)
		for _, inh := range readInherits(th, dirs) {
			walk(inh)
		}
	}
	walk(theme)
	if !seen["hicolor"] {
		out = append(out, "hicolor")
	}
	return out
}

func readInherits(theme string, dirs []string) []string {
	p := findIndexTheme(theme, dirs)
	if p == "" {
		return nil
	}
	return parseInherits(p)
}

func findIndexTheme(theme string, dirs []string) string {
	for _, dir := range dirs {
		p := filepath.Join(dir, "icons", theme, "index.theme")
		if fileOK(p) {
			return p
		}
	}
	return ""
}

// parseInherits reads Inherits= from the [Icon Theme] section.
func parseInherits(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	inIcon := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inIcon = strings.EqualFold(line, "[Icon Theme]")
			continue
		}
		if !inIcon {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "Inherits") {
			continue
		}
		return splitInheritList(v)
	}
	return nil
}

func splitInheritList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
