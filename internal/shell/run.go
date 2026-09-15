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
	"github.com/codemodify/worldr/internal/scanout"
	"github.com/codemodify/worldr/internal/syncobj"
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

	seat := ProbeSeat()
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
	scene.SetWorkspaces(opt.Workspaces)
	pixel := PackBGRA(opt.Color)

	p, err := openPresent(stdout, stderr, opt, seat)
	if err != nil {
		return err
	}
	defer p.close()

	w, h := p.size()
	fmt.Fprintf(stdout, "present: backend=%s size=%dx%d device=%s\n", p.name, w, h, p.device)
	fmt.Fprintf(stdout, "seat: %s\n", seat)
	if p.note != "" {
		fmt.Fprintln(stdout, p.note)
	}
	if TakesDisplay(Backend(p.name)) {
		fmt.Fprintln(stdout, "desktop: CompositeDesktop (panel, overview, launcher, workspaces, effects) on the real display path — same as nested.")
	}

	var srv *compositor.Server
	var x11WM *xwayland.XWM
	waylandName := ""
	x11Display := ""
	if opt.Compositor {
		var imp compositor.DMABufImport
		if p.vk != nil && p.vk.HasDMABuf() {
			imp = vkDMABuf{p.vk}
			if p.vk.IsDisplay() {
				fmt.Fprintln(stdout, "linux-dmabuf: Vulkan import + GPU sample on vk-display; KMS primary scanout when a fullscreen dmabuf is eligible (else blit)")
			} else {
				fmt.Fprintln(stdout, "linux-dmabuf: Vulkan import + readback enabled (shm remains fallback)")
			}
		} else if p.drm != nil {
			fmt.Fprintln(stdout, "linux-dmabuf: LINEAR mmap + KMS primary scanout for fullscreen ARGB/XRGB (tiled AddFB2 on Intel; NVIDIA/AMD best-effort)")
		} else {
			fmt.Fprintln(stdout, "linux-dmabuf: advertised; LINEAR mmap works, tiled GPU buffers need Vulkan import (unavailable on this device)")
		}
		if syncobj.TimelineAvailable() {
			if p.vk != nil && p.vk.HasTimeline() {
				fmt.Fprintln(stdout, "linux-drm-syncobj: wp_linux_drm_syncobj_manager_v1 advertised (Vulkan timeline wait before blit/sample/scanout; DRM ioctl fallback).")
			} else {
				fmt.Fprintln(stdout, "linux-drm-syncobj: wp_linux_drm_syncobj_manager_v1 advertised (DRM ioctl wait; Vulkan timeline unavailable on this device).")
			}
		} else {
			fmt.Fprintln(stdout, "linux-drm-syncobj: not advertised (no DRM SYNCOBJ_TIMELINE); implicit sync only")
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
			hostScale := 0.0
			if p.wl != nil {
				hostScale = p.wl.HostScale()
			}
			outScale := ResolveOutputScale(opt.Scale, hostScale)
			srv.SetOutputScale(outScale)
			if p.wl != nil && opt.Scale <= 0 {
				p.wl.OnHostScale(func(sc float64) {
					srv.SetOutputScale(sc)
				})
			}
			fmt.Fprintf(stdout, "wayland compositor: WAYLAND_DISPLAY=%s  (example: WAYLAND_DISPLAY=%s foot)\n",
				srv.DisplayName, srv.DisplayName)
			src := "auto 1.0"
			if opt.Scale > 0 {
				src = "--scale override"
			} else if p.wl != nil && hostScale > 0 {
				src = "nest host"
			}
			fmt.Fprintf(stdout, "output scale: %.2f (%s; preferred_scale=%d/120, wl_output.scale=%d). vk-display/drm stay 1.0 unless --scale.\n",
				outScale, src, srv.PreferredScale120ths(), srv.IntegerOutputScale())
			fmt.Fprintf(stdout, "socket: %s\n", srv.SocketPath)
			if nestedPresent(p.name) {
				fmt.Fprintf(stdout, "nested compositor: clients appear inside this window. Keep this WAYLAND_DISPLAY=%s for the host; use WAYLAND_DISPLAY=%s for foot/weston-simple-shm.\n",
					os.Getenv("WAYLAND_DISPLAY"), srv.DisplayName)
			}
			if p.wl != nil {
				wireHostClipboard(p.wl, srv, stdout)
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
						x11WM = xw.WM
						wireX11WM(srv, xw.WM)
						fmt.Fprintln(stdout, "xwayland: XWM + EWMH ready — titles/class, focus/stacking, override-redirect without SSD. Do not use the host DISPLAY.")
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
	var clientBtn clientButtonGate
	hostBtnDown := false

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
	var lnGate launcherGate
	fmt.Fprintln(stdout, "overview: F12 or panel grid toggles expose (Super+Tab if the host does not steal Super). Esc leaves overview (does not quit).")
	if opt.OverviewDemo {
		fmt.Fprintln(stdout, "overview: --overview-demo will auto-enter after the first window maps")
	}
	fmt.Fprintln(stdout, "panel: bottom bar always visible (worldr, apps, focused title, pager, grid, clock).")
	fmt.Fprintf(stdout, "launcher: F1 or Super+Space (or panel apps). %d apps from XDG .desktop (fallback if none). Enter/click spawns with this WAYLAND_DISPLAY. Esc closes the list.\n", len(ln.Items))
	fmt.Fprintf(stdout, "workspaces: %d desktops. Ctrl+Alt+←/→ switch (wraps); Ctrl+Alt+Shift+←/→ move the focused window and follow. Pager shows N/M + occupied dots. Empty desktops stay addressable. Overview is current-desktop only.\n", scene.WorkspaceCount())

	fmt.Fprintln(stdout, "running. Exit: Ctrl+Q, Ctrl+C, or --duration. Bare Q/Esc never quit while a client is on the desktop (type in foot freely). Esc on an empty desktop still quits.")
	if TakesDisplay(Backend(p.name)) {
		fmt.Fprintln(stdout, "WARNING: this process may own the VT display. Spare TTY: Ctrl+Alt+F3 + scripts/try-tty.sh. Back to Plasma: Ctrl+Alt+F1 or F2. See docs/RUN-ABOX.md")
	}

	frames := 0
	metaHeld := false
	ctrlHeld := false
	altHeld := false
	shiftHeld := false
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
		drainX11WM(scene, x11WM)
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
			// Nested host seat is authoritative for pointer (and keys).
			// OR-merging evdev Click with wl Click toggled the launcher
			// open then shut on one physical press.
			applyNestedPointer(ptr, p.wl.TakeInput())
			filterNestedButtons(ptr, &hostBtnDown)
		}
		now := time.Now()
		if demoArmed && scene.HasActors() && frames > 40 {
			ov.Open(now)
			actors := desktopActors(scene)
			ov.Select = focusedIndex(actors)
			demoArmed = false
			fmt.Fprintln(stdout, "overview: demo auto-enter (F12 or Esc to leave)")
		}
		lcons, spawn := handleLauncherKeys(&ln, ptr, now, &metaHeld, &lnGate)
		if spawn != nil {
			_ = SpawnClient(*spawn, waylandName, x11Display, stdout)
		}
		wcons := handleWorkspaceKeys(scene, ptr, now, &ctrlHeld, &altHeld, &shiftHeld)
		consume := handleOverviewKeys(&ov, ptr, scene, now, int(w), deskH, &metaHeld, ln.Open, &ctrlHeld, &altHeld)
		for code, ok := range wcons {
			if ok {
				consume[code] = true
			}
		}
		for code, ok := range lcons {
			if ok {
				consume[code] = true
			}
		}
		quit, qcons := handleQuitKeys(ptr, ctrlHeld, ov.Want || ln.Open, desktopHasClient(scene))
		for code, ok := range qcons {
			if ok {
				consume[code] = true
			}
		}
		if quit {
			fmt.Fprintln(stdout, "quit key")
			return nil
		}
		ptr.Quit = false
		if srv != nil && !ov.Live(now) && !ln.Open {
			for _, k := range ptr.Keys {
				if consume[k.Code] {
					continue
				}
				srv.KeyboardKey(compositor.ToXKBKeycode(k.Code, p.wl != nil), k.Pressed)
			}
		}
		if ln.Open && ptr.Click {
			item, ate := handleLauncherDesktopClick(&ln, &lnGate, ptr.X, ptr.Y, int(w), int(h), PanelH)
			if item != nil {
				_ = SpawnClient(*item, waylandName, x11Display, stdout)
			}
			if ate {
				ptr.Click = false
			}
		}
		if ptr.Click {
			prects := LayoutPanelWS(int(w), int(h), scene.WorkspaceCount())
			switch HitPanel(prects, ptr.X, ptr.Y) {
			case PanelHitLaunch:
				handlePanelAppsClick(&ln, &lnGate, now)
				ptr.Click = false
			case PanelHitOverview:
				ov.Toggle(now)
				if ov.Want {
					ov.Select = focusedIndex(desktopActors(scene))
				}
				ln.Close()
				ptr.Click = false
			case PanelHitPager:
				if i := HitPager(prects, ptr.X, ptr.Y); i >= 0 {
					scene.SwitchTo(i, now)
					if ov.Want {
						ov.Select = 0
					}
				}
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
			top := scene.HitTop(ptr.X, ptr.Y, decorations.TitleH, decorations.Border)
			if a != nil && a.X11Win != 0 {
				scene.Raise(a)
				if srv != nil && srv.X11OnFocus != nil {
					srv.X11OnFocus(a.X11Win)
				}
			}
			if a != nil && decorations.HitTitle(a, ptr.X, ptr.Y) && (top == nil || !top.NoChrome) {
				dragging = true
				drag = a
				dx, dy = ptr.X-a.X, ptr.Y-a.Y
			} else if srv != nil {
				srv.PointerButton(ptr.X, ptr.Y, true)
				clientBtn.onClientPress()
			}
		}
		if ptr.Release {
			lnGate.onRelease()
			dragging = false
			drag = nil
			if srv != nil && clientBtn.onRelease() {
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

		needFB := scene.HasActors() || srv != nil || nestedPresent(p.name) || TakesDisplay(Backend(p.name))
		if needFB {
			cur := CursorBlit{}
			if srv != nil {
				cx, cy, hx, hy, pix, cw, ch, cstride, shape, vis := srv.Cursor()
				cur = CursorBlit{X: cx, Y: cy, HX: hx, HY: hy, Pix: pix, W: cw, H: ch, Stride: cstride, Shape: shape, Visible: vis}
				if p.wl != nil {
					// Nest: host arrow is set on pointer enter. Hide it only
					// while a client provides a custom image/shape (and only
					// when that state changes — do not set_shape every frame).
					custom := srv.CursorCustom() && vis
					p.wl.SetHostCursorHidden(custom)
					cur.Visible = custom
				}
			}
			actors := scene.Actors()
			desk := desktopActors(scene)
			FillThemeIcons(actors, ln.Items)
			var ld *LauncherDraw
			if ln.Open {
				ld = ln.drawState()
			}
			vendor := uint32(0)
			if p.vk != nil {
				vendor = p.vk.VendorID()
			}
			dispPlanes := 0
			if p.vk != nil {
				dispPlanes = p.vk.DisplayPlanes()
			}
			assign := scanout.EvaluatePlanes(scanout.PlaneFrame{
				Frame: scanout.Frame{
					Backend:  p.name,
					ScreenW:  int(w),
					ScreenH:  int(h),
					Actors:   actors,
					WS:       scene.WorkspacePose(now),
					Overview: ov.Want || ov.Progress(now) > 0,
					Launcher: ln.Open,
					Theater:  opt.Effects,
					Now:      now,
					VendorID: vendor,
				},
				CursorVisible: cur.Visible,
				CursorW:       cur.W,
				CursorH:       cur.H,
				DisplayPlanes: dispPlanes,
			})
			if assign.Primary != nil {
				p.waitActorSync(assign.Primary)
				if err := p.tryScanout(assign.Primary); err == nil {
					p.signalActorRelease(assign.Primary)
					if !p.scanoutOn {
						fmt.Fprintln(stdout, "kms scanout: primary dmabuf (atomic/SetCrtc); panel/cursor skipped while fullscreen")
						p.scanoutOn = true
					}
					frames++
					continue
				} else if !p.scanoutMiss {
					fmt.Fprintf(stdout, "kms scanout fallback: %v\n", err)
					p.scanoutMiss = true
				}
			}
			if p.scanoutOn {
				p.restoreScanout()
				p.scanoutOn = false
			}
			overlay := assign.Overlay
			if overlay != nil {
				p.waitActorSync(overlay)
				if err := p.tryOverlay(overlay); err != nil {
					if !p.overlayMiss {
						fmt.Fprintf(stdout, "kms overlay fallback: %v\n", err)
						p.overlayMiss = true
					}
					overlay = nil
				} else if !p.overlayOn {
					fmt.Fprintln(stdout, "kms overlay: windowed dmabuf on overlay plane; primary keeps desktop")
					p.overlayOn = true
				}
			}
			if overlay == nil && p.overlayOn {
				p.disableOverlay()
				p.overlayOn = false
			}
			if overlay != nil {
				overlay.PlaneSkip = true
			}
			hwCursor := assign.Cursor && p.drm != nil
			if hwCursor {
				if err := p.tryCursor(cur); err != nil {
					if !p.cursorMiss {
						fmt.Fprintf(stdout, "kms cursor fallback: %v\n", err)
						p.cursorMiss = true
					}
					hwCursor = false
				} else {
					if !p.cursorOn {
						fmt.Fprintln(stdout, "kms cursor: hardware cursor plane")
						p.cursorOn = true
					}
					cur.Visible = false
				}
			}
			if !hwCursor && p.cursorOn {
				p.disableCursor()
				p.cursorOn = false
			}
			gpuOverlay := p.vk != nil && p.name == string(BackendVKDisplay) && ov.Progress(now) == 0
			ch := ChromeDraw{
				PanelH:     PanelH,
				Clock:      ClockString(now),
				Brand:      "worldr",
				Title:      FocusedTitle(desk),
				LaunchOn:   ln.Open,
				OverviewOn: ov.Want,
				Launcher:   ld,
				WS:         scene.WorkspacePose(now),
				Occupied:   scene.Occupied(),
			}
			if fa := focusedActor(desk); fa != nil {
				ch.Icon, ch.IconW, ch.IconH, ch.IconStride = fa.IconPix, fa.IconW, fa.IconH, fa.IconStride
			}
			p.waitActorsSync(actors)
			CompositeDesktop(fb, stride, int(w), int(h), pixel, actors, opt.SSD, cur,
				Theater{Now: now, Tier: opt.Effects},
				OverviewDraw{T: ov.Progress(now), Select: ov.Select},
				ch, gpuOverlay)
			layers := gpuLayers(actors, ch, int(w), gpuOverlay)
			if err := p.upload(fb, uint32(stride), layers); err != nil {
				return err
			}
			p.signalActorsRelease(actors)
			if overlay != nil {
				overlay.PlaneSkip = false
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
	name        string
	device      string
	note        string
	w, h        uint32
	vk          *native.VK
	drm         *native.DRM
	wl          *wlclient.Window
	color       [4]float32
	scanoutOn   bool
	scanoutMiss bool
	overlayOn   bool
	overlayMiss bool
	cursorOn    bool
	cursorMiss  bool
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
	p.restoreScanout()
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
		return p.upload(fb, uint32(w*4), nil)
	}
}

func gpuLayers(actors []*engine.Actor, ch ChromeDraw, screenW int, on bool) []native.GPULayer {
	if !on {
		return nil
	}
	var out []native.GPULayer
	for _, a := range actors {
		if a == nil || a.GPUSlot <= 0 || a.ScaledBuffer() || a.PlaneSkip {
			continue
		}
		ox, show := ch.WS.OffsetFor(a.Workspace, screenW)
		if !show {
			continue
		}
		out = append(out, native.GPULayer{Slot: a.GPUSlot, X: a.X + ox, Y: a.Y, W: a.Width, H: a.Height})
	}
	return out
}

func (p *presenter) tryScanout(a *engine.Actor) error {
	if p == nil || a == nil || a.ScanFD <= 0 {
		return fmt.Errorf("no scan fd")
	}
	sw, sh := p.size()
	if uint32(a.Width) != sw || uint32(a.Height) != sh {
		return fmt.Errorf("buffer size != CRTC")
	}
	if p.drm != nil {
		return p.drm.ScanoutDMABuf(a.ScanFD, sw, sh, a.ScanFourcc, a.ScanMod, a.ScanOff, a.ScanStride)
	}
	if p.name == string(BackendVKDisplay) {
		// VK_KHR_display typically holds DRM master on the same card; a
		// second fd cannot atomic-commit. Keep the GPU blit path.
		return fmt.Errorf("VK_KHR_display holds DRM master; GPU blit fallback")
	}
	return fmt.Errorf("no KMS session")
}

func (p *presenter) restoreScanout() {
	if p != nil && p.drm != nil && p.drm.ScanoutActive() {
		_ = p.drm.RestoreScanout()
	}
	p.disableOverlay()
	p.disableCursor()
}

func (p *presenter) tryOverlay(a *engine.Actor) error {
	if p == nil || a == nil || a.ScanFD <= 0 {
		return fmt.Errorf("no overlay fd")
	}
	if p.drm == nil {
		if p.name == string(BackendVKDisplay) {
			return fmt.Errorf("VK_KHR_display holds DRM master; compose fallback")
		}
		return fmt.Errorf("no KMS session")
	}
	bw, bh := a.PixelSize()
	return p.drm.OverlayDMABuf(a.ScanFD, uint32(bw), uint32(bh), a.ScanFourcc, a.ScanMod, a.ScanOff, a.ScanStride, a.X, a.Y)
}

func (p *presenter) disableOverlay() {
	if p != nil && p.drm != nil {
		_ = p.drm.OverlayDisable()
	}
}

func (p *presenter) tryCursor(cur CursorBlit) error {
	if p == nil || p.drm == nil {
		return fmt.Errorf("no KMS session")
	}
	if !cur.Visible || cur.W < 1 || cur.H < 1 || len(cur.Pix) == 0 {
		return fmt.Errorf("no cursor pixels")
	}
	x := cur.X - cur.HX
	y := cur.Y - cur.HY
	return p.drm.CursorARGB(x, y, uint32(cur.W), uint32(cur.H), cur.Pix, uint32(cur.Stride))
}

func (p *presenter) disableCursor() {
	if p != nil && p.drm != nil {
		_ = p.drm.CursorDisable()
	}
}

func (p *presenter) waitActorSync(a *engine.Actor) {
	if p == nil || a == nil || a.AcqFD <= 0 {
		return
	}
	f := syncobj.Fence{FD: a.AcqFD, Point: a.AcqPoint}
	var w syncobj.Waiter
	if p.vk != nil {
		w = p.vk
	}
	_ = syncobj.WaitAcquire(f, 100*time.Millisecond, w)
}

func (p *presenter) waitActorsSync(actors []*engine.Actor) {
	for _, a := range actors {
		p.waitActorSync(a)
	}
}

func (p *presenter) signalActorRelease(a *engine.Actor) {
	if a == nil || a.RelFD <= 0 {
		return
	}
	_ = syncobj.SignalFence(syncobj.Fence{FD: a.RelFD, Point: a.RelPoint})
	f := syncobj.Fence{FD: a.RelFD}
	f.CloseFD()
	a.RelFD, a.RelPoint = 0, 0
}

func (p *presenter) signalActorsRelease(actors []*engine.Actor) {
	for _, a := range actors {
		p.signalActorRelease(a)
	}
}

func (p *presenter) upload(bgra []byte, stride uint32, layers []native.GPULayer) error {
	switch {
	case p.vk != nil && p.name == string(BackendVKDisplay):
		if len(layers) > 0 {
			return p.vk.UploadPresentLayers(bgra, stride, layers)
		}
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

func (v vkDMABuf) RetainDMABuf(width, height, fourcc uint32, modifier uint64, planes []compositor.DMABufPlane) (int, error) {
	np := make([]native.DMABufPlane, len(planes))
	for i, p := range planes {
		np[i] = native.DMABufPlane{FD: p.FD, Offset: p.Offset, Stride: p.Stride}
	}
	return v.VK.RetainDMABuf(width, height, fourcc, modifier, np)
}

func (v vkDMABuf) ReleaseDMABuf(slot int) {
	v.VK.ReleaseDMABuf(slot)
}

func (v vkDMABuf) CanGPUComposite() bool {
	return v.VK != nil && v.VK.IsDisplay()
}

func (v vkDMABuf) HasTimeline() bool {
	return v.VK != nil && v.VK.HasTimeline()
}

func (v vkDMABuf) WaitTimeline(fd int, point uint64, timeoutNS uint64) error {
	if v.VK == nil {
		return fmt.Errorf("vulkan session closed")
	}
	return v.VK.WaitTimeline(fd, point, timeoutNS)
}

func openPresent(stdout, stderr io.Writer, opt Options, seat Seat) (*presenter, error) {
	order := []Backend{opt.Backend}
	if opt.Backend == BackendAuto {
		order = AutoPresentOrder(seat.Wayland, seat.X11, opt.TakeOverDisplay, len(seat.DRMCards) > 0)
		if seat.Wayland && !opt.TakeOverDisplay {
			fmt.Fprintln(stdout, "auto: WAYLAND_DISPLAY set; using nested compositor (pass --take-over-display for vk-display/drm)")
		} else if seat.X11 && !opt.TakeOverDisplay {
			fmt.Fprintln(stdout, "auto: DISPLAY set without Wayland; staying headless (unset DISPLAY on a spare TTY for vk-display)")
		} else if len(seat.DRMCards) > 0 {
			fmt.Fprintln(stdout, "auto: no graphical session env; trying vk-display then drm (spare TTY / DRM master)")
		} else {
			fmt.Fprintln(stdout, "auto: no /dev/dri/card*; headless")
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
		if err := ValidateDRMCard(opt.Card); err != nil {
			return nil, err
		}
		if !HasDRM() {
			return nil, fmt.Errorf("no /dev/dri/card* — vk-display needs a GPU node and DRM master. Spare TTY: Ctrl+Alt+F3 then scripts/try-tty.sh")
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
		if err := ValidateDRMCard(opt.Card); err != nil {
			return nil, err
		}
		if !HasDRM() && opt.Card == "" {
			return nil, fmt.Errorf("no /dev/dri/card* — drm backend needs a KMS device. Spare TTY: Ctrl+Alt+F3 then scripts/try-tty.sh")
		}
		d, err := native.OpenDRM(opt.Card)
		if err != nil {
			return nil, err
		}
		w, h, _ := d.Size()
		return &presenter{name: string(b), device: d.Card(), w: w, h: h, drm: d, color: opt.Color,
			note: "DRM/KMS dumb buffer present (CPU blit). Fullscreen ARGB/XRGB dmabuf uses primary-plane scanout when eligible."}, nil
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

func wireX11WM(srv *compositor.Server, wm *xwayland.XWM) {
	if srv == nil || wm == nil {
		return
	}
	srv.X11OnMap = func(w, h int) (compositor.X11MapHints, bool) {
		sh, ok := wm.ConsumeMap(w, h)
		return surfaceHintsToMap(sh), ok
	}
	srv.X11OnFocus = wm.FocusWindow
}

func drainX11WM(scene *engine.Scene, wm *xwayland.XWM) {
	if wm == nil || scene == nil {
		return
	}
	changes, activates := wm.Drain()
	for _, sh := range changes {
		compositor.ApplyX11Hints(scene, surfaceHintsToMap(sh))
	}
	for _, win := range activates {
		for _, a := range scene.Actors() {
			if a != nil && a.X11Win == win {
				scene.FocusActor(a)
				scene.Raise(a)
				break
			}
		}
	}
}

func surfaceHintsToMap(h xwayland.SurfaceHints) compositor.X11MapHints {
	return compositor.X11MapHints{
		Win:          h.Win,
		TransientFor: h.TransientFor,
		Title:        h.Title,
		AppID:        h.AppID,
		NoChrome:     h.NoChrome,
		X:            h.X,
		Y:            h.Y,
		W:            h.W,
		H:            h.H,
	}
}
