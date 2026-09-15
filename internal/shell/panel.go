package shell

import (
	"time"

	"github.com/codemodify/worldr/internal/decorations"
	"github.com/codemodify/worldr/internal/engine"
)

// PanelH is the reserved bottom chrome strip (always composited).
const PanelH = 36

const (
	colPanel      uint32 = 0xff121826
	colPanelLine  uint32 = 0xff7cf0e8
	colPanelBtn   uint32 = 0xff2a3350
	colPanelBtnOn uint32 = 0xff4a2260
	colText       uint32 = 0xffe8eef8
	colTextDim    uint32 = 0xff9aa8c0
	colBrand      uint32 = 0xff7cf0e8
	colLaunchBg   uint32 = 0xff1a2233
	colLaunchSel  uint32 = 0xff2a3350
	colLaunchDim  uint32 = 0xff000000
)

// PanelZone is a click target on the shell panel.
type PanelZone int

const (
	PanelHitNone PanelZone = iota
	PanelHitLaunch
	PanelHitOverview
	PanelHitPager
	PanelHitBar
)

// PanelRects are hit/draw boxes in screen space.
type PanelRects struct {
	Bar, Brand, Launch, Title, Overview, Clock, Pager, Label engine.GridCell
	Dots                                                     []engine.GridCell
}

// ChromeDraw is the shell DE chrome passed into CompositeDesktop.
type ChromeDraw struct {
	PanelH     int
	Clock      string
	Brand      string
	Title      string
	LaunchOn   bool
	OverviewOn bool
	Launcher   *LauncherDraw
	WS         engine.WorkspaceDraw
	Occupied   []bool
	Icon       []byte
	IconW      int
	IconH      int
	IconStride int
}

// LaunchIcon is one overlay row icon (theme PNG or empty → default glyph).
type LaunchIcon struct {
	Pix          []byte
	W, H, Stride int
}

// LauncherDraw is the in-shell command overlay.
type LauncherDraw struct {
	Items  []string
	Icons  []LaunchIcon
	Select int
}

// LayoutPanel places brand, apps, title, grid, clock on a bottom bar.
func LayoutPanel(w, h int) PanelRects {
	return LayoutPanelWS(w, h, 0)
}

// LayoutPanelWS is LayoutPanel plus a pager of n dots (n<2 hides the pager).
func LayoutPanelWS(w, h, workspaces int) PanelRects {
	if w <= 0 || h <= 0 {
		return PanelRects{}
	}
	y0 := h - PanelH
	if y0 < 0 {
		y0 = 0
	}
	ph := h - y0
	bar := engine.GridCell{X: 0, Y: y0, W: w, H: ph}
	btnH := 22
	if btnH > ph-4 {
		btnH = ph - 4
	}
	if btnH < 1 {
		btnH = 1
	}
	by := y0 + (ph-btnH)/2
	pad := 8
	brandW := engine.TextWidth("worldr", 2) + 4
	brand := engine.GridCell{X: pad, Y: by, W: brandW, H: btnH}
	launchW := engine.TextWidth("apps", 2) + 16
	launch := engine.GridCell{X: brand.X + brand.W + 8, Y: by, W: launchW, H: btnH}
	clockW := engine.TextWidth("00:00:00", 2) + 4
	clock := engine.GridCell{X: w - pad - clockW, Y: by, W: clockW, H: btnH}
	if clock.X < 0 {
		clock.X = 0
	}
	ovW := engine.TextWidth("grid", 2) + 16
	overview := engine.GridCell{X: clock.X - 8 - ovW, Y: by, W: ovW, H: btnH}
	if overview.X < launch.X+launch.W {
		overview.X = launch.X + launch.W + 4
	}
	pagerW := 0
	labelW := 0
	n := workspaces
	if n >= engine.WorkspaceMin {
		labelW = engine.TextWidth("4/4", 2) + 6
		pagerW = labelW + n*16
	}
	pager := engine.GridCell{X: overview.X - 8 - pagerW, Y: by, W: pagerW, H: btnH}
	if pager.X < launch.X+launch.W {
		pager.X = launch.X + launch.W + 4
	}
	label := engine.GridCell{}
	if labelW > 0 {
		label = engine.GridCell{X: pager.X, Y: by, W: labelW, H: btnH}
	}
	dots := make([]engine.GridCell, 0, n)
	if pagerW > 0 {
		for i := 0; i < n; i++ {
			dots = append(dots, engine.GridCell{
				X: pager.X + labelW + i*16 + 3,
				Y: by + (btnH-10)/2,
				W: 10, H: 10,
			})
		}
	}
	titleX := launch.X + launch.W + 12
	titleRight := overview.X
	if pagerW > 0 {
		titleRight = pager.X
	}
	titleW := titleRight - 12 - titleX
	if titleW < 0 {
		titleW = 0
	}
	title := engine.GridCell{X: titleX, Y: by, W: titleW, H: btnH}
	return PanelRects{Bar: bar, Brand: brand, Launch: launch, Title: title, Overview: overview, Clock: clock, Pager: pager, Label: label, Dots: dots}
}

// HitPanel returns the chrome zone under (x,y).
func HitPanel(r PanelRects, x, y int) PanelZone {
	if !inCell(r.Bar, x, y) {
		return PanelHitNone
	}
	if inCell(r.Launch, x, y) {
		return PanelHitLaunch
	}
	if inCell(r.Overview, x, y) {
		return PanelHitOverview
	}
	if HitPager(r, x, y) >= 0 {
		return PanelHitPager
	}
	return PanelHitBar
}

// HitPager returns the desktop index under (x,y), or -1.
func HitPager(r PanelRects, x, y int) int {
	for i, d := range r.Dots {
		if inCell(d, x, y) {
			return i
		}
	}
	return -1
}

func inCell(c engine.GridCell, x, y int) bool {
	return x >= c.X && x < c.X+c.W && y >= c.Y && y < c.Y+c.H
}

// ClockString is local HH:MM:SS for the panel.
func ClockString(now time.Time) string {
	return now.Format("15:04:05")
}

func focusedActor(actors []*engine.Actor) *engine.Actor {
	for _, a := range actors {
		if a != nil && a.Focused {
			return a
		}
	}
	return nil
}

// FocusedTitle is the focused actor's title or app id.
func FocusedTitle(actors []*engine.Actor) string {
	for _, a := range actors {
		if a != nil && a.Focused {
			if a.Title != "" {
				return a.Title
			}
			return a.AppID
		}
	}
	return ""
}

func drawPanel(dst []byte, stride, w, h int, ch ChromeDraw) {
	if ch.PanelH <= 0 || w <= 0 || h <= 0 {
		return
	}
	r := LayoutPanelWS(w, h, ch.WS.Count)
	engine.FillRect(dst, stride, w, h, r.Bar.X, r.Bar.Y, r.Bar.W, r.Bar.H, colPanel)
	engine.FillRect(dst, stride, w, h, r.Bar.X, r.Bar.Y, r.Bar.W, 2, colPanelLine)
	brand := ch.Brand
	if brand == "" {
		brand = "worldr"
	}
	ty := textY(r.Brand, 2)
	engine.DrawText(dst, stride, w, h, r.Brand.X, ty, brand, colBrand, 2)
	drawBtn(dst, stride, w, h, r.Launch, "apps", ch.LaunchOn)
	drawPagerDots(dst, stride, w, h, r, ch.WS.Active, ch.WS.Count, ch.Occupied)
	drawBtn(dst, stride, w, h, r.Overview, "grid", ch.OverviewOn)
	titleX := r.Title.X
	if r.Title.W > 20 && (ch.Title != "" || len(ch.Icon) > 0) {
		iz := 16
		if iz > r.Title.H-4 {
			iz = r.Title.H - 4
		}
		if iz >= 8 {
			iy := r.Title.Y + (r.Title.H-iz)/2
			a := &engine.Actor{IconPix: ch.Icon, IconW: ch.IconW, IconH: ch.IconH, IconStride: ch.IconStride}
			decorations.DrawIcon(dst, stride, w, h, titleX, iy, iz, a)
			titleX += iz + 6
		}
	}
	if ch.Title != "" && r.Title.W > 8 {
		tw := r.Title.W - (titleX - r.Title.X)
		if tw > 8 {
			engine.DrawText(dst, stride, w, h, titleX, textY(r.Title, 2), truncateTo(ch.Title, tw, 2), colTextDim, 2)
		}
	}
	if ch.Clock != "" {
		engine.DrawText(dst, stride, w, h, r.Clock.X, textY(r.Clock, 2), ch.Clock, colText, 2)
	}
}

func drawPagerDots(dst []byte, stride, w, h int, r PanelRects, active, count int, occupied []bool) {
	if r.Label.W > 0 {
		lbl := engine.WorkspaceLabel(active, count)
		if lbl != "" {
			engine.DrawText(dst, stride, w, h, r.Label.X, textY(r.Label, 2), lbl, colBrand, 2)
		}
	}
	for i, d := range r.Dots {
		pix := colTextDim
		if i < len(occupied) && occupied[i] {
			pix = colText
		}
		if i == active {
			pix = colBrand
		}
		pad := 2
		if i == active {
			pad = 0
		}
		engine.FillRect(dst, stride, w, h, d.X+pad, d.Y+pad, d.W-2*pad, d.H-2*pad, pix)
	}
}

func drawBtn(dst []byte, stride, w, h int, c engine.GridCell, label string, on bool) {
	if c.W <= 0 || c.H <= 0 {
		return
	}
	pix := colPanelBtn
	if on {
		pix = colPanelBtnOn
	}
	engine.FillRect(dst, stride, w, h, c.X, c.Y, c.W, c.H, pix)
	lw := engine.TextWidth(label, 2)
	tx := c.X + (c.W-lw)/2
	engine.DrawText(dst, stride, w, h, tx, textY(c, 2), label, colText, 2)
}

func textY(c engine.GridCell, scale int) int {
	th := engine.TextHeight(scale)
	return c.Y + (c.H-th)/2
}

func truncateTo(s string, maxW, scale int) string {
	if engine.TextWidth(s, scale) <= maxW {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		if engine.TextWidth(string(runes)+".", scale) <= maxW {
			return string(runes) + "."
		}
	}
	return ""
}

func drawLauncher(dst []byte, stride, w, h int, panelH int, ln LauncherDraw) {
	deskH := h - panelH
	if deskH < 1 {
		deskH = h
	}
	engine.FillRectAlpha(dst, stride, w, h, 0, 0, w, deskH, colLaunchDim, 0.45)
	card, rows := LayoutLauncher(len(ln.Items), w, h, panelH)
	engine.FillRect(dst, stride, w, h, card.X, card.Y, card.W, card.H, colLaunchBg)
	engine.FillRect(dst, stride, w, h, card.X, card.Y, card.W, 3, colPanelLine)
	engine.DrawText(dst, stride, w, h, card.X+12, card.Y+8, "launch", colBrand, 2)
	for i, row := range rows {
		label := ""
		if i < len(ln.Items) {
			label = ln.Items[i]
		}
		if i == ln.Select {
			engine.FillRect(dst, stride, w, h, row.X, row.Y, row.W, row.H, colLaunchSel)
		}
		tx := row.X + 12
		if i < len(ln.Icons) {
			iz := 16
			if iz > row.H-4 {
				iz = row.H - 4
			}
			if iz >= 8 {
				iy := row.Y + (row.H-iz)/2
				a := &engine.Actor{IconPix: ln.Icons[i].Pix, IconW: ln.Icons[i].W, IconH: ln.Icons[i].H, IconStride: ln.Icons[i].Stride}
				decorations.DrawIcon(dst, stride, w, h, tx, iy, iz, a)
				tx += iz + 8
			}
		}
		engine.DrawText(dst, stride, w, h, tx, textY(row, 2), label, colText, 2)
	}
}

// LayoutLauncher returns the overlay card and one row per item (above the panel).
func LayoutLauncher(n, screenW, screenH, panelH int) (card engine.GridCell, rows []engine.GridCell) {
	deskH := screenH - panelH
	if deskH < 1 {
		deskH = screenH
	}
	rowH := 28
	head := 32
	cardW := 400
	if screenW-48 < cardW {
		cardW = screenW - 48
	}
	if cardW < 80 {
		cardW = screenW
		if cardW < 1 {
			cardW = 1
		}
	}
	if n < 0 {
		n = 0
	}
	cardH := head + n*rowH + 8
	if cardH > deskH-24 {
		cardH = deskH - 24
	}
	if cardH < head {
		cardH = head
	}
	card = engine.GridCell{
		X: (screenW - cardW) / 2,
		Y: (deskH - cardH) / 2,
		W: cardW,
		H: cardH,
	}
	if card.X < 0 {
		card.X = 0
	}
	if card.Y < 8 {
		card.Y = 8
	}
	rows = make([]engine.GridCell, 0, n)
	for i := 0; i < n; i++ {
		y := card.Y + head + i*rowH
		if y+rowH > card.Y+card.H {
			break
		}
		rows = append(rows, engine.GridCell{
			X: card.X + 8,
			Y: y,
			W: card.W - 16,
			H: rowH,
		})
	}
	return card, rows
}

// HitLauncher returns the row index, or -1. inside is true if (x,y) is in the card.
func HitLauncher(card engine.GridCell, rows []engine.GridCell, x, y int) (index int, inside bool) {
	if inCell(card, x, y) {
		inside = true
	}
	for i, r := range rows {
		if inCell(r, x, y) {
			return i, true
		}
	}
	return -1, inside
}

func usableHeight(h, panelH int) int {
	if panelH > 0 && panelH < h {
		return h - panelH
	}
	return h
}
