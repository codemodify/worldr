//go:build linux && cgo

package apps

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type toolkitClient struct {
	t       *testing.T
	server  *Server
	cmd     *exec.Cmd
	done    chan error
	ended   bool
	logPath string
	latest  []Surface
}

func startToolkit(t *testing.T, name string, buildArgs func(string) []string, shared ...*Server) *toolkitClient {
	t.Helper()
	if os.Getenv("WORLDR_TEST_TOOLKITS") != "1" {
		t.Skip("set WORLDR_TEST_TOOLKITS=1 for isolated toolkit tests")
	}
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skip(err)
	}
	var server *Server
	if len(shared) > 0 {
		server = shared[0]
	} else {
		server, err = Open(900, 560)
		if err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "client.log")
	log, err := os.Create(logPath)
	if err != nil {
		if len(shared) == 0 {
			server.Close()
		}
		t.Fatal(err)
	}
	cmd := exec.Command(path, buildArgs(dir)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "WAYLAND_DEBUG=1", "WAYLAND_DISPLAY="+server.Socket(), "QT_QPA_PLATFORM=wayland", "HOME="+dir, "XDG_CONFIG_HOME="+filepath.Join(dir, "config"), "XDG_CACHE_HOME="+filepath.Join(dir, "cache"), "XDG_DATA_HOME="+filepath.Join(dir, "data"), "DBUS_SESSION_BUS_ADDRESS=unix:path="+filepath.Join(dir, "no-session-bus"))
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		log.Close()
		if len(shared) == 0 {
			server.Close()
		}
		t.Fatal(err)
	}
	c := &toolkitClient{t: t, server: server, cmd: cmd, done: make(chan error, 1), logPath: logPath}
	go func() { c.done <- cmd.Wait() }()
	t.Cleanup(func() {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if !c.ended {
			<-c.done
		}
		log.Close()
		if len(shared) == 0 {
			server.Close()
		}
		if t.Failed() {
			b, _ := os.ReadFile(logPath)
			if len(b) > 16000 {
				b = b[len(b)-16000:]
			}
			t.Log(string(b))
		}
	})
	return c
}
func (c *toolkitClient) until(why string, predicate func([]Surface) bool) {
	c.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var exitedAt time.Time
	var exitErr error
	for time.Now().Before(deadline) {
		v, err := c.server.Poll()
		if err != nil {
			c.t.Fatal(err)
		}
		c.latest = v
		if predicate(v) {
			return
		}
		select {
		case err := <-c.done:
			c.ended = true
			exitedAt = time.Now()
			exitErr = err
		default:
		}
		if !exitedAt.IsZero() && time.Since(exitedAt) > 250*time.Millisecond {
			c.t.Fatalf("%s: client exited: %v", why, exitErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
	c.t.Fatalf("timed out: %s", why)
}
func (c *toolkitClient) settle() {
	c.t.Helper()
	end := time.Now().Add(200 * time.Millisecond)
	c.until("settling client", func([]Surface) bool { return time.Now().After(end) })
}
func (c *toolkitClient) key(code uint32) {
	c.t.Helper()
	if err := c.server.Key(code, true, 10, 0, 0, 0, 0); err != nil {
		c.t.Fatal(err)
	}
	if err := c.server.Key(code, false, 11, 0, 0, 0, 0); err != nil {
		c.t.Fatal(err)
	}
}
func (c *toolkitClient) click(id uint64, x, y float32, button uint32) {
	c.t.Helper()
	if err := c.server.Pointer(id, x, y); err != nil {
		c.t.Fatal(err)
	}
	if err := c.server.Button(button, true, 10); err != nil {
		c.t.Fatal(err)
	}
	if err := c.server.Button(button, false, 11); err != nil {
		c.t.Fatal(err)
	}
}
func (c *toolkitClient) capture(name string) {
	if dir := os.Getenv("WORLDR_TOOLKIT_CAPTURE"); dir != "" && len(c.latest) > 0 {
		os.MkdirAll(dir, 0700)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			c.t.Fatal(err)
		}
		v := c.latest[0]
		err = png.Encode(f, &image.RGBA{Pix: v.Pixels, Stride: v.Width * 4, Rect: image.Rect(0, 0, v.Width, v.Height)})
		f.Close()
		if err != nil {
			c.t.Fatal(err)
		}
	}
}
func (c *toolkitClient) logContains(text string) bool {
	b, _ := os.ReadFile(c.logPath)
	return bytes.Contains(b, []byte(text))
}
func (c *toolkitClient) logCount(text string) int {
	b, _ := os.ReadFile(c.logPath)
	return strings.Count(string(b), text)
}

func TestChromiumPopupInputAndDismissal(t *testing.T) {
	c := startToolkit(t, "chromium", func(dir string) []string {
		page := filepath.Join(dir, "popup.html")
		html := `<title>popup ready</title><style>body{background:#15753a;color:white;font:24px sans-serif}input,select,button{position:absolute;left:20px;width:240px;height:40px;font:20px sans-serif}input{top:100px}select{top:180px}button{top:260px}</style><h1>WORLD RENDERER POPUP TEST</h1><input autofocus oninput="document.title='typed:'+this.value"><select onchange="document.title='selected:'+this.value"><option>alpha</option><option>beta</option><option>gamma</option></select><button onclick="document.title='clicked:behind'">underlying action</button>`
		if err := os.WriteFile(page, []byte(html), 0600); err != nil {
			t.Fatal(err)
		}
		return []string{"--ozone-platform=wayland", "--disable-gpu", "--user-data-dir=" + filepath.Join(dir, "profile"), "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-default-apps", "--disable-features=MediaRouter", "--app=file://" + page}
	})
	c.until("local page mapped", func(v []Surface) bool { return len(v) == 1 && v[0].Title == "popup ready" })
	if !c.logContains(`"xdg_wm_base", 3`) {
		t.Fatal("Chromium did not negotiate xdg_wm_base version 3")
	}
	id := c.latest[0].ID
	if err := c.server.Focus(id); err != nil {
		t.Fatal(err)
	}
	c.settle()
	before := c.latest[0]
	c.click(id, 100, 200, 272)
	c.settle()
	c.capture("chromium-dropdown")
	if !c.logContains(".get_popup(") {
		t.Fatal("dropdown did not use xdg_popup")
	}
	if bytes.Equal(before.Pixels, c.latest[0].Pixels) {
		t.Fatal("popup did not appear in parent snapshot")
	}
	c.key(108)
	c.key(28)
	c.until("dropdown receives keyboard", func(v []Surface) bool { return len(v) == 1 && v[0].Title == "selected:beta" })
	c.click(id, 100, 200, 272)
	c.settle()
	c.click(id, 100, 300, 272)
	c.until("dropdown receives mapped pointer", func(v []Surface) bool { return len(v) == 1 && v[0].Title == "selected:gamma" })
	c.click(id, 500, 300, 273)
	c.settle()
	c.capture("chromium-context")
	c.key(1)
	c.settle()
	c.click(id, 500, 300, 273)
	c.settle()
	// Clicking the underlying button first dismisses a grabbing popup and must
	// not also activate the page underneath it.
	c.click(id, 100, 280, 272)
	c.settle()
	if c.latest[0].Title == "clicked:behind" {
		t.Fatal("popup dismissal clicked through to parent")
	}
	c.click(id, 100, 280, 272)
	c.until("parent receives click after dismissal", func(v []Surface) bool { return len(v) == 1 && v[0].Title == "clicked:behind" })
	c.click(id, 500, 300, 273)
	c.settle()
	if err := c.server.CloseSurface(id); err != nil {
		t.Fatal(err)
	}
	c.until("closing parent clears popup tree", func(v []Surface) bool { return len(v) == 0 })
	if c.logContains("error 1:") || c.logContains("popup surfaces are not supported") {
		t.Fatal("client protocol error")
	}
	b, _ := os.ReadFile(c.logPath)
	t.Logf("verified %d popup creations, %d reactive positioners and %d explicit repositions",
		strings.Count(string(b), ".get_popup("), strings.Count(string(b), ".set_reactive("),
		strings.Count(string(b), ".reposition("))
}

func TestKonsoleDialogAndPopupDisconnect(t *testing.T) {
	c := startToolkit(t, "konsole", func(dir string) []string {
		return []string{"--separate", "--nofork", "--workdir", dir, "-e", "sh", "-c", "printf 'WORLD RENDERER DIALOG TEST\\n'; sleep 30"}
	})
	c.until("Konsole mapped", func(v []Surface) bool { return len(v) == 1 })
	id := c.latest[0].ID
	if err := c.server.Focus(id); err != nil {
		t.Fatal(err)
	}
	c.settle()
	// Konsole's Configure or Rename Tab action opens an ordinary Qt dialog.
	for _, e := range []struct {
		code uint32
		down bool
		mods uint32
	}{{29, true, 4}, {56, true, 12}, {31, true, 12}, {31, false, 12}, {56, false, 4}, {29, false, 0}} {
		if err := c.server.Key(e.code, e.down, 10, e.mods, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	c.until("Qt dialog mapped separately", func(v []Surface) bool { return len(v) == 2 })
	var dialog Surface
	for _, v := range c.latest {
		if v.ID != id {
			dialog = v
		}
	}
	if dialog.PID != c.latest[0].PID || dialog.Width == 0 || len(dialog.Pixels) == 0 {
		t.Fatalf("invalid dialog metadata: %+v", dialog)
	}
	t.Logf("dialog title=%q app_id=%q", dialog.Title, dialog.AppID)
	if err := c.server.Focus(dialog.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.server.CloseSurface(dialog.ID); err != nil {
		t.Fatal(err)
	}
	c.until("dialog dismissal preserves parent", func(v []Surface) bool { return len(v) == 1 && v[0].ID == id })
	parentRevision := c.latest[0].Revision
	if err := c.server.Focus(id); err != nil {
		t.Fatal(err)
	}
	c.until("parent commits after regaining focus", func(v []Surface) bool {
		return len(v) == 1 && v[0].ID == id && v[0].Revision > parentRevision
	})
	before := c.latest[0]
	popups := c.logCount(".get_popup(")
	c.click(id, 300, 180, 273)
	c.until("Qt context popup composited", func(v []Surface) bool {
		return len(v) == 1 && v[0].ID == id && c.logCount(".get_popup(") > popups &&
			!bytes.Equal(before.Pixels, v[0].Pixels)
	})
	c.capture("konsole-context")
	popups = c.logCount(".get_popup(")
	if err := c.server.Pointer(id, 450, 297); err != nil {
		t.Fatal(err)
	}
	c.until("Qt nested submenu opens", func([]Surface) bool { return c.logCount(".get_popup(") > popups })
	c.settle()
	c.capture("konsole-submenu")
	beforeHover := c.latest[0]
	if err := c.server.Pointer(id, 680, 298); err != nil {
		t.Fatal(err)
	}
	c.settle()
	c.capture("konsole-submenu-hover")
	different := 0
	for y := 280; y < 380; y++ {
		for x := 590; x < 890; x++ {
			i := (y*beforeHover.Width + x) * 4
			if !bytes.Equal(beforeHover.Pixels[i:i+4], c.latest[0].Pixels[i:i+4]) {
				different++
			}
		}
	}
	if different < 500 {
		t.Fatalf("nested submenu did not receive pointer outside parent bounds: %d pixels changed", different)
	}
	c.key(1)
	c.settle()
	c.key(1)
	c.settle()
	beforeDisconnect := c.latest[0]
	popups = c.logCount(".get_popup(")
	c.click(id, 300, 180, 273)
	c.until("Qt context popup composited before disconnect", func(v []Surface) bool {
		return len(v) == 1 && v[0].ID == id && c.logCount(".get_popup(") > popups &&
			!bytes.Equal(beforeDisconnect.Pixels, v[0].Pixels)
	})
	if err := syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	c.until("disconnect clears parent and popup", func(v []Surface) bool { return len(v) == 0 })
	if _, err := c.server.Poll(); err != nil {
		t.Fatal(err)
	}
	if err := c.server.Focus(0); err != nil {
		t.Fatal(err)
	}
	t.Logf("verified %d reactive positioners and %d explicit repositions",
		c.logCount(".set_reactive("), c.logCount(".reposition("))
}
