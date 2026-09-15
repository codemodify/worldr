package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/codemodify/worldr/internal/compositor"
	"github.com/codemodify/worldr/internal/compositor/xwayland"
	"github.com/codemodify/worldr/internal/decorations"
	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/input"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/platform/linux/wlclient"
	"github.com/codemodify/worldr/internal/version"
)

// Run is the worldr-shell entry after flags are parsed.
func Run(stdout, stderr io.Writer, opt Options) error {
	fmt.Fprintf(stdout, "worldr-shell %s\n", version.String())

	if opt.ListDevices {
		if !native.Available() {
			return fmt.Errorf("vulkan listing needs linux + CGO")
		}
		s, err := native.ListDevices()
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, s)
		return nil
	}

	if err := CheckTakeover(opt.Backend, opt.TakeOverDisplay); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if opt.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opt.Duration)
		defer cancel()
	}

	scene := engine.NewScene()
	scene.SetTheater(opt.Effects)
	pixel := PackBGRA(opt.Color)

	p, err := openPresent(stdout, stderr, opt)
	if err != nil {
		return err
	}
	defer p.close()

	w, h := p.size()
	fmt.Fprintf(stdout, "present: backend=%s size=%dx%d device=%s\n", p.name, w, h, p.device)
	if p.note != "" {
		fmt.Fprintln(stdout, p.note)
	}

	var srv *compositor.Server
	waylandName := ""
	x11Display := ""
	if opt.Compositor {
		var imp compositor.DMABufImport
		if p.vk != nil && p.vk.HasDMABuf() {
			imp = vkDMABuf{p.vk}
			fmt.Fprintln(stdout, "linux-dmabuf: Vulkan import + readback enabled (shm remains fallback)")
		} else {
			fmt.Fprintln(stdout, "linux-dmabuf: advertised; LINEAR mmap works, tiled GPU buffers need Vulkan import (unavailable on this device)")
		}
		deskH := usableHeight(int(h), PanelH)
		if deskH < 1 {
			deskH = 1
		}
		s, err := compositor.Listen(opt.WaylandDisplay, scene, int(w), deskH, imp)
		if err != nil {
			fmt.Fprintf(stderr, "compositor listen failed (continuing as clear-only): %v\n", err)
		} else {
			srv = s
			waylandName = srv.DisplayName
			defer srv.Close()
			fmt.Fprintf(stdout, "wayland compositor: WAYLAND_DISPLAY=%s  (example: WAYLAND_DISPLAY=%s foot)\n",
				srv.DisplayName, srv.DisplayName)
			fmt.Fprintf(stdout, "socket: %s\n", srv.SocketPath)
			if nestedPresent(p.name) {
				fmt.Fprintf(stdout, "nested compositor: clients appear inside this window. Keep this WAYLAND_DISPLAY=%s for the host; use WAYLAND_DISPLAY=%s for foot/weston-simple-shm.\n",
					os.Getenv("WAYLAND_DISPLAY"), srv.DisplayName)
			}
			if opt.XWayland {
				runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
				xw, err := xwayland.Start(srv.DisplayName, runtimeDir, srv.Dispatch)
				if err != nil {
					fmt.Fprintf(stderr, "xwayland: %v\n", err)
					fmt.Fprintln(stderr, "xwayland: install xorg-xwayland (Arch) and retry. Wayland clients still work.")
				} else {
					defer xw.Close()
					x11Display = xw.Display
					fmt.Fprintf(stdout, "xwayland: DISPLAY=%s  (example: DISPLAY=%s xeyes)\n", xw.Display, xw.Display)
					if xw.WM != nil {
						fmt.Fprintln(stdout, "xwayland: tiny XWM ready — managed X11 windows map as worldr actors + SSD. Do not use the host DISPLAY.")
					} else {
						fmt.Fprintln(stderr, "xwayland: running without XWM; override-redirect clients only. Do not use the host DISPLAY.")
					}
				}
			}
		}
	} else if nestedPresent(p.name) {
		fmt.Fprintln(stdout, "nested wayland-client debug path: --compositor=false, clear window only.")
	}
	if opt.XWayland && srv == nil {
		fmt.Fprintln(stderr, "xwayland: ignored (--compositor is off or listen failed)")
	}

	ptr := input.Open(int(w), int(h))
	defer ptr.Close()

	fb := make([]byte, int(w)*int(h)*4)
	stride := int(w) * 4
	dragging := false
	var drag *engine.Actor
	dx, dy := 0, 0

	switch opt.Effects {
	case engine.TierOff:
		fmt.Fprintln(stdout, "theater: effects=off")
	case engine.TierLow:
		fmt.Fprintln(stdout, "theater: effects=low (fade-only map/unmap)")
	default:
		fmt.Fprintf(stdout, "theater: effects=high (scale+fade map %s / unmap %s, focus pulse)\n",
			engine.MapInDuration, engine.MapOutDuration)
	}

	var ov Overview
	ln := Launcher{Items: Catalog(x11Display != "")}
	fmt.Fprintln(stdout, "overview: F12 or panel grid toggles expose (Super+Tab if the host does not steal Super). Esc leaves overview; Esc/Q outside it quits.")
	if opt.OverviewDemo {
		fmt.Fprintln(stdout, "overview: --overview-demo will auto-enter after the first window maps")
	}
	fmt.Fprintln(stdout, "panel: bottom bar always visible (worldr, apps, focused title, grid, clock).")
	fmt.Fprintln(stdout, "launcher: F1 or Super+Space (or panel apps). Enter/click spawns with this WAYLAND_DISPLAY. Esc closes the list.")

	fmt.Fprintln(stdout, "running. Exit: Ctrl+C, or --duration, or Esc/Q on an evdev keyboard.")
	if TakesDisplay(Backend(p.name)) {
		fmt.Fprintln(stdout, "WARNING: this process may own the VT display. Prefer a spare TTY (Ctrl+Alt+F2). See docs/RUN-ABOX.md")
	}

	frames := 0
	metaHeld := false
	demoArmed := opt.OverviewDemo
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintf(stdout, "stopped after %d frames (%v)\n", frames, ctx.Err())
			return nil
		case <-ticker.C:
		}
		if srv != nil {
			srv.Dispatch()
		}
		scene.Sweep(time.Now())
		w, h = p.size()
		deskH := usableHeight(int(h), PanelH)
		if deskH < 1 {
			deskH = 1
		}
		if srv != nil {
			srv.ScreenW, srv.ScreenH = int(w), deskH
		}
		need := int(w) * int(h) * 4
		if len(fb) != need {
			fb = make([]byte, need)
			stride = int(w) * 4
			ptr.W, ptr.H = int(w), int(h)
		}
		ptr.Poll()
		if p.wl != nil {
			in := p.wl.TakeInput()
			ptr.X, ptr.Y = in.X, in.Y
			if in.Click {
				ptr.Click = true
			}
			if in.Release {
				ptr.Release = true
			}
			for _, k := range in.Keys {
				ptr.Keys = append(ptr.Keys, input.Key{Code: k.Code, Pressed: k.Pressed})
			}
		}
		now := time.Now()
		if demoArmed && scene.HasActors() && frames > 40 {
			ov.Open(now)
			actors := scene.Actors()
			ov.Select = focusedIndex(actors)
			demoArmed = false
			fmt.Fprintln(stdout, "overview: demo auto-enter (F12 or Esc to leave)")
		}
		lcons, spawn := handleLauncherKeys(&ln, ptr, now, &metaHeld)
		if spawn != nil {
			_ = SpawnClient(*spawn, waylandName, x11Display, stdout)
		}
		consume := handleOverviewKeys(&ov, ptr, scene, now, int(w), deskH, &metaHeld, ln.Open)
		for code, ok := range lcons {
			if ok {
				consume[code] = true
			}
		}
		if ptr.Quit && !ov.Want && !ln.Open {
			fmt.Fprintln(stdout, "quit key")
			return nil
		}
		ptr.Quit = false
		if srv != nil && !ov.Live(now) && !ln.Open {
			for _, k := range ptr.Keys {
				if consume[k.Code] {
					continue
				}
				srv.KeyboardKey(k.Code, k.Pressed)
			}
		}
		if ln.Open && ptr.Click && ptr.Y < deskH {
			card, rows := LayoutLauncher(len(ln.Items), int(w), int(h), PanelH)
			idx, inside := HitLauncher(card, rows, ptr.X, ptr.Y)
			if idx >= 0 && idx < len(ln.Items) {
				it := ln.Items[idx]
				ln.Select = idx
				ln.Close()
				_ = SpawnClient(it, waylandName, x11Display, stdout)
			} else if !inside {
				ln.Close()
			}
			ptr.Click = false
		}
		if ptr.Click {
			switch HitPanel(LayoutPanel(int(w), int(h)), ptr.X, ptr.Y) {
			case PanelHitLaunch:
				ln.Toggle()
				ptr.Click = false
			case PanelHitOverview:
				ov.Toggle(now)
				if ov.Want {
					ov.Select = focusedIndex(scene.Actors())
				}
				ln.Close()
				ptr.Click = false
			case PanelHitBar:
				ptr.Click = false
			}
		}
		if ov.Want && ptr.Click {
			_ = pickOverview(&ov, scene, ptr.X, ptr.Y, int(w), deskH, now)
			ptr.Click = false
		}
		if ptr.Click {
			a := scene.FocusAt(ptr.X, ptr.Y, decorations.TitleH, decorations.Border)
			if a != nil && decorations.HitTitle(a, ptr.X, ptr.Y) {
				dragging = true
				drag = a
				dx, dy = ptr.X-a.X, ptr.Y-a.Y
			} else if srv != nil {
				srv.PointerButton(ptr.X, ptr.Y, true)
			}
		}
		if ptr.Release {
			dragging = false
			drag = nil
			if srv != nil && !ov.Want && !ln.Open {
				srv.PointerButton(ptr.X, ptr.Y, false)
			}
		}
		if dragging && drag != nil && !ov.Want && !ln.Open {
			drag.X = ptr.X - dx
			drag.Y = ptr.Y - dy
		} else if srv != nil && !ov.Live(now) && !ln.Open {
			srv.PointerMotion(ptr.X, ptr.Y)
		}
		if srv != nil {
			srv.SetPointerPos(ptr.X, ptr.Y)
		}

		needFB := scene.HasActors() || srv != nil || nestedPresent(p.name) || p.name == string(BackendDRM)
		if needFB {
			cur := CursorBlit{}
			if srv != nil {
				cx, cy, hx, hy, pix, cw, ch, cstride, shape, vis := srv.Cursor()
				cur = CursorBlit{X: cx, Y: cy, HX: hx, HY: hy, Pix: pix, W: cw, H: ch, Stride: cstride, Shape: shape, Visible: vis}
			}
			actors := scene.Actors()
			var ld *LauncherDraw
			if ln.Open {
				ld = &LauncherDraw{Items: ln.labels(), Select: ln.Select}
			}
			CompositeDesktop(fb, stride, int(w), int(h), pixel, actors, opt.SSD, cur,
				Theater{Now: now, Tier: opt.Effects},
				OverviewDraw{T: ov.Progress(now), Select: ov.Select},
				ChromeDraw{
					PanelH:     PanelH,
					Clock:      ClockString(now),
					Brand:      "worldr",
					Title:      FocusedTitle(actors),
					LaunchOn:   ln.Open,
					OverviewOn: ov.Want,
					Launcher:   ld,
				})
			if err := p.upload(fb, uint32(stride)); err != nil {
				return err
			}
		} else {
			if err := p.clear(opt.Color); err != nil {
				return err
			}
		}
		frames++
	}
}

type presenter struct {
	name   string
	device string
	note   string
	w, h   uint32
	vk     *native.VK
	drm    *native.DRM
	wl     *wlclient.Window
	color  [4]float32
}

func nestedPresent(name string) bool {
	return name == string(BackendWaylandClient) || name == string(BackendNested)
}

func (p *presenter) size() (uint32, uint32) {
	if p.wl != nil {
		w, h, _ := p.wl.Size()
		if w > 0 && h > 0 {
			p.w, p.h = uint32(w), uint32(h)
		}
	}
	return p.w, p.h
}

func (p *presenter) close() {
	if p.vk != nil {
		p.vk.Close()
	}
	if p.drm != nil {
		p.drm.Close()
	}
	if p.wl != nil {
		p.wl.Close()
	}
}

func (p *presenter) clear(c [4]float32) error {
	switch {
	case p.vk != nil && p.name == string(BackendVKDisplay):
		return p.vk.ClearPresent(c[0], c[1], c[2], c[3])
	case p.vk != nil && p.name == string(BackendHeadless):
		_, err := p.vk.HeadlessClear(c[0], c[1], c[2], c[3])
		return err
	default:
		w, h := int(p.w), int(p.h)
		fb := make([]byte, w*h*4)
		engine.FillBGRA(fb, w*4, w, h, PackBGRA(c))
		return p.upload(fb, uint32(w*4))
	}
}

func (p *presenter) upload(bgra []byte, stride uint32) error {
	switch {
	case p.vk != nil && p.name == string(BackendVKDisplay):
		return p.vk.UploadPresent(bgra, stride)
	case p.drm != nil:
		return p.drm.PresentBGRA(bgra, stride)
	case p.wl != nil:
		return p.wl.Present(bgra, int(stride))
	case p.vk != nil && p.name == string(BackendHeadless):
		_, err := p.vk.HeadlessClear(p.color[0], p.color[1], p.color[2], p.color[3])
		return err
	default:
		return nil
	}
}

type vkDMABuf struct{ *native.VK }

func (v vkDMABuf) ImportDMABuf(width, height, fourcc uint32, modifier uint64, planes []compositor.DMABufPlane) ([]byte, int, error) {
	np := make([]native.DMABufPlane, len(planes))
	for i, p := range planes {
		np[i] = native.DMABufPlane{FD: p.FD, Offset: p.Offset, Stride: p.Stride}
	}
	return v.VK.ImportDMABuf(width, height, fourcc, modifier, np)
}

func openPresent(stdout, stderr io.Writer, opt Options) (*presenter, error) {
	order := []Backend{opt.Backend}
	if opt.Backend == BackendAuto {
		wl, x11 := GraphicalSession()
		if (wl || x11) && !opt.TakeOverDisplay {
			if wl {
				order = []Backend{BackendWaylandClient, BackendHeadless}
			} else {
				order = []Backend{BackendHeadless}
			}
			fmt.Fprintln(stdout, "auto: graphical session detected; using nested/headless (pass --take-over-display for DRM/Vulkan display)")
		} else {
			order = []Backend{BackendVKDisplay, BackendDRM, BackendWaylandClient, BackendHeadless}
		}
	}

	var errs []error
	for _, b := range order {
		p, err := openOne(opt, b)
		if err == nil {
			return p, nil
		}
		switch b {
		case BackendVKDisplay:
			err = hintVKDisplay(err)
		case BackendWaylandClient, BackendNested:
			err = hintWaylandClient(err)
		case BackendDRM:
			err = hintDRM(err)
		}
		errs = append(errs, fmt.Errorf("%s: %w", b, err))
		if opt.Backend != BackendAuto {
			return nil, err
		}
		fmt.Fprintf(stderr, "backend %s: %v\n", b, err)
	}
	return nil, fmt.Errorf("no present backend worked: %v", errs)
}

func openOne(opt Options, b Backend) (*presenter, error) {
	switch b {
	case BackendVKDisplay:
		if !native.Available() {
			return nil, fmt.Errorf("cgo/vulkan not in this binary")
		}
		vk, err := native.OpenVK(true, 0, 0)
		if err != nil {
			return nil, err
		}
		w, h := vk.Size()
		return &presenter{name: string(b), device: vk.DeviceName(), w: w, h: h, vk: vk, color: opt.Color,
			note: "VK_KHR_display GPU clear/present. First bring-up target: Intel / Mesa."}, nil
	case BackendDRM:
		if !native.Available() {
			return nil, fmt.Errorf("cgo/drm not in this binary")
		}
		d, err := native.OpenDRM(opt.Card)
		if err != nil {
			return nil, err
		}
		w, h, _ := d.Size()
		return &presenter{name: string(b), device: d.Card(), w: w, h: h, drm: d, color: opt.Color,
			note: "DRM/KMS dumb buffer present (CPU blit). Vulkan is used when --backend=vk-display."}, nil
	case BackendWaylandClient, BackendNested:
		title := "worldr-shell (nested compositor)"
		note := "SAFE DEMO: nested window on the host session + worldr compositor socket. Run clients with the printed WAYLAND_DISPLAY (not the host's)."
		if !opt.Compositor {
			title = "worldr-shell (nested debug)"
			note = "DEBUG ONLY: nested Wayland client via wl_shm. --compositor=false, no socket."
		}
		win, err := wlclient.Open(title, opt.ClientWidth, opt.ClientHeight, opt.FullscreenClient)
		if err != nil {
			return nil, err
		}
		w, h, _ := win.Size()
		p := &presenter{name: string(b), device: "wayland-client", w: uint32(w), h: uint32(h), wl: win, color: opt.Color, note: note}
		if native.Available() {
			vk, err := native.OpenVK(false, 64, 64)
			if err != nil {
				p.note += " Vulkan import unavailable: " + err.Error()
			} else {
				p.vk = vk
				if vk.DeviceName() != "" {
					p.device = vk.DeviceName() + "+wayland-client"
				}
				p.note += " Vulkan dmabuf import available for nested GPU clients."
			}
		}
		return p, nil
	case BackendHeadless:
		p := &presenter{name: string(b), device: "none", w: 1280, h: 720, color: opt.Color,
			note: "headless: no DRM. Tries Vulkan offscreen clear if an ICD exists."}
		if native.Available() {
			vk, err := native.OpenVK(false, 64, 64)
			if err != nil {
				p.note += " Vulkan instance/device: " + err.Error()
				return p, nil
			}
			p.vk = vk
			p.device = vk.DeviceName()
			if p.device == "" {
				p.device = "headless-vk"
			}
			p.note += " Vulkan headless clear enabled."
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unknown backend %s", b)
	}
}
