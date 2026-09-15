package shell

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// maxLauncherItems caps the overlay so it fits a nested 720p window.
const maxLauncherItems = 32

// DesktopDirs is the XDG applications search path:
// $XDG_DATA_HOME/applications then each $XDG_DATA_DIRS/applications.
func DesktopDirs() []string {
	var dirs []string
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		dirs = append(dirs, filepath.Join(d, "applications"))
	} else if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".local/share/applications"))
	}
	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	for _, d := range strings.Split(dataDirs, ":") {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		dirs = append(dirs, filepath.Join(d, "applications"))
	}
	return dirs
}

// LoadCatalog scans dirs for .desktop entries. Empty scan → fallback list.
func LoadCatalog(dirs []string, currentDesktop string, xwayland bool) []LaunchItem {
	seen := map[string]struct{}{}
	var out []LaunchItem
	for _, dir := range dirs {
		ents, err := scanDesktopDir(dir, currentDesktop, xwayland)
		if err != nil {
			continue
		}
		for _, it := range ents {
			id := it.ID
			if id == "" {
				id = it.Bin + "\x00" + it.Label
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, it)
		}
	}
	if len(out) == 0 {
		return fallbackCatalog(xwayland)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label)
	})
	if len(out) > maxLauncherItems {
		out = out[:maxLauncherItems]
	}
	return out
}

// Catalog is the launcher list: XDG .desktop scan, or the small fallback.
func Catalog(xwayland bool) []LaunchItem {
	return LoadCatalog(DesktopDirs(), os.Getenv("XDG_CURRENT_DESKTOP"), xwayland)
}

func fallbackCatalog(xwayland bool) []LaunchItem {
	out := []LaunchItem{
		{Label: "foot", Bin: "foot"},
		{Label: "weston-simple-shm", Bin: "weston-simple-shm"},
	}
	if xwayland {
		out = append(out,
			LaunchItem{Label: "xeyes", Bin: "xeyes", X11: true},
			LaunchItem{Label: "xterm", Bin: "xterm", X11: true},
			LaunchItem{Label: "xcalc", Bin: "xcalc", X11: true},
		)
	}
	return out
}

func scanDesktopDir(dir, currentDesktop string, xwayland bool) ([]LaunchItem, error) {
	var out []LaunchItem
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".desktop") {
			return nil
		}
		it, ok := parseDesktopFile(path, currentDesktop, xwayland)
		if ok {
			rel, err := filepath.Rel(dir, path)
			if err == nil {
				it.ID = desktopID(rel)
			} else {
				it.ID = d.Name()
			}
			out = append(out, it)
		}
		return nil
	})
	return out, err
}

func desktopID(rel string) string {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, ".desktop")
	return strings.ReplaceAll(rel, "/", "-")
}

type desktopEntry struct {
	Type       string
	Name       string
	Exec       string
	Icon       string
	TryExec    string
	NoDisplay  bool
	Hidden     bool
	Terminal   bool
	OnlyShowIn []string
	NotShowIn  []string
	inEntry    bool
	sawDesktop bool
}

func parseDesktopFile(path, currentDesktop string, xwayland bool) (LaunchItem, bool) {
	f, err := os.Open(path)
	if err != nil {
		return LaunchItem{}, false
	}
	defer f.Close()

	var e desktopEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimSuffix(sc.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			e.inEntry = strings.EqualFold(line, "[Desktop Entry]")
			if e.inEntry {
				e.sawDesktop = true
			}
			continue
		}
		if !e.inEntry {
			continue
		}
		key, val, ok := splitDesktopKV(line)
		if !ok {
			continue
		}
		switch key {
		case "Type":
			e.Type = val
		case "Name":
			if e.Name == "" {
				e.Name = val
			}
		case "Exec":
			e.Exec = val
		case "Icon":
			e.Icon = val
		case "TryExec":
			e.TryExec = val
		case "NoDisplay":
			e.NoDisplay = desktopBool(val)
		case "Hidden":
			e.Hidden = desktopBool(val)
		case "Terminal":
			e.Terminal = desktopBool(val)
		case "OnlyShowIn":
			e.OnlyShowIn = splitDesktopList(val)
		case "NotShowIn":
			e.NotShowIn = splitDesktopList(val)
		}
	}
	if !e.sawDesktop || e.Hidden || e.NoDisplay || e.Terminal {
		return LaunchItem{}, false
	}
	if e.Type != "" && !strings.EqualFold(e.Type, "Application") {
		return LaunchItem{}, false
	}
	if e.Name == "" || e.Exec == "" {
		return LaunchItem{}, false
	}
	if !desktopShown(e.OnlyShowIn, e.NotShowIn, currentDesktop) {
		return LaunchItem{}, false
	}
	if e.TryExec != "" {
		if _, err := exec.LookPath(e.TryExec); err != nil {
			return LaunchItem{}, false
		}
	}
	argv := parseDesktopExec(e.Exec)
	if len(argv) == 0 {
		return LaunchItem{}, false
	}
	x11 := looksX11(argv[0])
	if x11 && !xwayland {
		return LaunchItem{}, false
	}
	return LaunchItem{
		Label: e.Name,
		Bin:   argv[0],
		Args:  argv[1:],
		Icon:  e.Icon,
		X11:   x11,
	}, true
}

func desktopShown(only, not []string, current string) bool {
	if current == "" {
		return true
	}
	have := splitDesktopList(strings.ReplaceAll(current, ":", ";"))
	if len(not) > 0 && intersectsFold(not, have) {
		return false
	}
	if len(only) > 0 && !intersectsFold(only, have) {
		return false
	}
	return true
}

func intersectsFold(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if strings.EqualFold(x, y) {
				return true
			}
		}
	}
	return false
}

func splitDesktopKV(line string) (key, val string, ok bool) {
	i := strings.IndexByte(line, '=')
	if i <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:i])
	if key == "" || strings.Contains(key, "[") {
		// skip localized keys (Name[en]=…)
		return "", "", false
	}
	return key, unquoteDesktop(strings.TrimSpace(line[i+1:])), true
}

func unquoteDesktop(s string) string {
	s = strings.ReplaceAll(s, `\s`, " ")
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")
	s = strings.ReplaceAll(s, `\\`, `\`)
	return s
}

func desktopBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

func splitDesktopList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ";") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func looksX11(bin string) bool {
	switch strings.ToLower(filepath.Base(bin)) {
	case "xeyes", "xterm", "uxterm", "xcalc", "xclock", "xlogo":
		return true
	default:
		return false
	}
}

// parseDesktopExec splits Exec= and strips XDG field codes (%f %F %u %U …).
func parseDesktopExec(execLine string) []string {
	var out []string
	for _, tok := range tokenizeExec(execLine) {
		tok = stripFieldCodes(tok)
		if tok == "" {
			continue
		}
		out = append(out, tok)
	}
	return out
}

func tokenizeExec(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
		case unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func stripFieldCodes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+1 < len(s) {
			if s[i+1] == '%' {
				b.WriteByte('%')
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return strings.TrimSpace(b.String())
}
