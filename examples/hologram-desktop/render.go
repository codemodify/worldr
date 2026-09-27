package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
	"golang.org/x/image/vector"
)

type point struct{ x, y float64 }
type diskCanvas struct {
	d                                  *diskDesktop
	img                                *image.RGBA
	sx, sy, scale                      float64
	background, cyan, violet           color.NRGBA
	gold, foreground, muted, gridColor color.NRGBA
	err                                error
}

func (d *diskDesktop) paint() error {
	return d.paintFrame(true)
}

func (d *diskDesktop) paintFrame(reuse bool) error {
	pixels := d.pixels
	if pixels == nil || pixels.Bounds().Size() != image.Pt(d.width, d.height) {
		pixels = image.NewRGBA(image.Rect(0, 0, d.width, d.height))
	}
	c := &diskCanvas{d: d, img: pixels, sx: float64(d.width) / initialWidth, sy: float64(d.height) / initialHeight}
	c.scale = min(c.sx, c.sy)
	c.background, c.cyan, c.violet = d.selected.Color("background"), d.selected.Color("accent"), d.selected.Color("accent-alt")
	c.gold, c.foreground, c.muted = d.selected.Color("warning"), d.selected.Color("text"), d.selected.Color("muted")
	c.gridColor = d.selected.Color("graph-grid")
	if c.gridColor.A == 0 {
		c.gridColor = mix(c.background, c.cyan, .2)
	}
	c.background.A = 255
	d.controls = d.controls[:0]
	if reuse && d.background != nil {
		copy(pixels.Pix, d.background.Pix)
	} else {
		draw.Draw(pixels, pixels.Bounds(), image.NewUniform(c.background), image.Point{}, draw.Src)
		c.particles()
		c.tableBackdrop()
		if reuse {
			d.background = image.NewRGBA(pixels.Bounds())
			copy(d.background.Pix, pixels.Pix)
		}
	}
	c.layer(0, 0, 145, reuse, func() { c.menuBar(); c.toolbar() })
	c.layer(1, 145, 420, reuse, c.table)
	c.layer(2, 420, 896, reuse, c.partitionMap)
	c.layer(3, 896, 1000, reuse, c.legend)
	c.popup()
	if c.err != nil {
		return c.err
	}
	if err := d.controller.SetControlsWithin(d.controls, pixels.Bounds()); err != nil {
		return err
	}
	d.pixels, d.dirty = pixels, true
	return nil
}

func (c *diskCanvas) check(err error) {
	if c.err == nil {
		c.err = err
	}
}
func (c *diskCanvas) bounds(x, y, w, h float64) image.Rectangle {
	return image.Rect(int(math.Round(x*c.sx)), int(math.Round(y*c.sy)), int(math.Round((x+w)*c.sx)), int(math.Round((y+h)*c.sy))).Intersect(c.img.Bounds())
}
func opaque(v color.NRGBA) color.RGBA { return color.RGBA{v.R, v.G, v.B, 255} }
func mix(a, b color.NRGBA, fraction float64) color.NRGBA {
	channel := func(x, y uint8) uint8 { return uint8(float64(x)*(1-fraction) + float64(y)*fraction) }
	return color.NRGBA{channel(a.R, b.R), channel(a.G, b.G), channel(a.B, b.B), 255}
}
func (c *diskCanvas) rect(x, y, w, h float64, ink color.NRGBA, alpha float64) {
	ink.A = uint8(min(1, max(0, alpha)) * 255)
	draw.Draw(c.img, c.bounds(x, y, w, h), image.NewUniform(ink), image.Point{}, draw.Over)
}
func (c *diskCanvas) painter(size float64) *nativeui.Painter {
	px := max(10, int(math.Round(size*c.scale*float64(c.d.theme.Typography.Size)/15)))
	if p := c.d.painters[px]; p != nil {
		return p
	}
	theme := c.d.theme
	theme.Typography.Size = px
	theme.Typography.LineHeight = px + 3
	p, err := nativeui.NewPainter(theme)
	c.check(err)
	if err == nil {
		c.d.painters[px] = p
	}
	return p
}
func (c *diskCanvas) text(x, y, w, h, size float64, value string, ink color.NRGBA) {
	if p := c.painter(size); p != nil {
		c.check(p.DrawLabel(c.img, c.bounds(x, y, w, h), value, nativeui.LabelStyle{Color: opaque(ink)}))
	}
}

// A small glyph mask halo keeps the luminous type readable without blurring
// the actual letterforms. All colors come from the selected skin package.
func (c *diskCanvas) glowText(x, y, w, h, size float64, value string, ink color.NRGBA) {
	bounds := c.bounds(x, y, w, h)
	if bounds.Empty() {
		return
	}
	p := c.painter(size)
	if p == nil {
		return
	}
	layer := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	c.check(p.DrawLabel(layer, layer.Bounds(), value, nativeui.LabelStyle{Color: opaque(mix(ink, c.foreground, .28))}))
	radius := max(2, int(math.Round(3*c.scale)))
	mask := glyphHalo(layer, radius)
	draw.DrawMask(c.img, bounds, image.NewUniform(ink), image.Point{}, mask, image.Point{}, draw.Over)
	draw.Draw(c.img, bounds, layer, image.Point{}, draw.Over)
}
func (c *diskCanvas) centered(x, y, w, h, size float64, value string, ink color.NRGBA) {
	if p := c.painter(size); p != nil {
		c.check(p.DrawLabel(c.img, c.bounds(x, y, w, h), value, nativeui.LabelStyle{Color: opaque(ink), Align: nativeui.AlignCenter}))
	}
}
func (c *diskCanvas) control(id string, kind nativeui.Kind, x, y, w, h float64, label string, selected bool) nativeui.Control {
	control := nativeui.Control{ID: id, Kind: kind, Bounds: c.bounds(x, y, w, h), Label: label, State: nativeui.State{Selected: selected}}
	c.d.controls = append(c.d.controls, control)
	return c.d.controller.Decorate(control)
}
func (c *diskCanvas) recipe(kind string, control nativeui.Control) {
	if p := c.painter(18); p != nil {
		c.check(p.DrawControlBackground(c.img, kind, control.Bounds, control.State))
	}
}

func (c *diskCanvas) blend(x, y int, ink color.NRGBA, alpha float64) {
	if x < 0 || y < 0 || x >= c.img.Rect.Dx() || y >= c.img.Rect.Dy() || alpha <= 0 {
		return
	}
	alpha = min(1, alpha)
	i := y*c.img.Stride + x*4
	c.img.Pix[i] = uint8(float64(c.img.Pix[i])*(1-alpha) + float64(ink.R)*alpha)
	c.img.Pix[i+1] = uint8(float64(c.img.Pix[i+1])*(1-alpha) + float64(ink.G)*alpha)
	c.img.Pix[i+2] = uint8(float64(c.img.Pix[i+2])*(1-alpha) + float64(ink.B)*alpha)
	c.img.Pix[i+3] = 255
}

// Software line glow uses a bounded brush along the segment rather than a
// full-frame blur. Static geometry is repainted only when interaction changes.
func (c *diskCanvas) line(a, b point, ink color.NRGBA, strength float64, glow bool) {
	x1, y1, x2, y2 := a.x*c.sx, a.y*c.sy, b.x*c.sx, b.y*c.sy
	dx, dy := x2-x1, y2-y1
	if glow {
		radius := max(4., 11*c.scale)
		lengthSquared := dx*dx + dy*dy
		core := mix(ink, c.foreground, .62)
		for yy := max(0, int(min(y1, y2)-radius)); yy <= min(c.img.Rect.Dy()-1, int(max(y1, y2)+radius)); yy++ {
			for xx := max(0, int(min(x1, x2)-radius)); xx <= min(c.img.Rect.Dx()-1, int(max(x1, x2)+radius)); xx++ {
				t := 0.
				if lengthSquared > 0 {
					t = max(0, min(1, ((float64(xx)+.5-x1)*dx+(float64(yy)+.5-y1)*dy)/lengthSquared))
				}
				distance := math.Hypot(float64(xx)+.5-(x1+t*dx), float64(yy)+.5-(y1+t*dy))
				if distance > radius {
					continue
				}
				spread := max(.7, c.scale)
				halo := (math.Exp(-distance*distance/(4*spread*spread))*.42 + math.Exp(-distance*distance/(38*spread*spread))*.13) * strength
				c.blend(xx, yy, ink, halo)
				c.blend(xx, yy, core, max(0, 1.15-distance)*strength*.9)
			}
		}
		return
	}
	steps := max(1, int(math.Ceil(math.Max(math.Abs(dx), math.Abs(dy)))))
	radius := 1
	for i := 0; i <= steps; i++ {
		x, y := x1+dx*float64(i)/float64(steps), y1+dy*float64(i)/float64(steps)
		for yy := int(y) - radius; yy <= int(y)+radius; yy++ {
			for xx := int(x) - radius; xx <= int(x)+radius; xx++ {
				distance := math.Hypot(float64(xx)+.5-x, float64(yy)+.5-y)
				alpha := max(0, 1-distance) * strength * .8
				c.blend(xx, yy, ink, alpha)
			}
		}
	}
}
func (c *diskCanvas) outline(points []point, ink color.NRGBA, alpha float64, glow bool) {
	for i, p := range points {
		c.line(p, points[(i+1)%len(points)], ink, alpha, glow)
	}
}
func (c *diskCanvas) polygon(points []point, ink color.NRGBA, alpha float64) {
	minX, minY, maxX, maxY := points[0].x, points[0].y, points[0].x, points[0].y
	for _, p := range points {
		minX, minY, maxX, maxY = min(minX, p.x), min(minY, p.y), max(maxX, p.x), max(maxY, p.y)
	}
	bounds := c.bounds(minX, minY, maxX-minX+1, maxY-minY+1)
	if bounds.Empty() {
		return
	}
	r := vector.NewRasterizer(bounds.Dx(), bounds.Dy())
	for i, p := range points {
		x, y := float32(p.x*c.sx)-float32(bounds.Min.X), float32(p.y*c.sy)-float32(bounds.Min.Y)
		if i == 0 {
			r.MoveTo(x, y)
		} else {
			r.LineTo(x, y)
		}
	}
	r.ClosePath()
	ink.A = uint8(min(1, max(0, alpha)) * 255)
	r.Draw(c.img, bounds, image.NewUniform(ink), image.Point{})
}
func chamfer(x, y, w, h, cut float64) []point {
	return []point{{x + cut, y}, {x + w - cut, y}, {x + w, y + cut}, {x + w, y + h - cut}, {x + w - cut, y + h}, {x + cut, y + h}, {x, y + h - cut}, {x, y + cut}}
}
func (c *diskCanvas) frame(x, y, w, h float64, ink color.NRGBA, bright float64) {
	c.outline(chamfer(x, y, w, h, min(12, w/3, h/3)), ink, bright, true)
	if w > 16 && h > 16 {
		c.outline(chamfer(x+5, y+5, w-10, h-10, min(9, (w-10)/3, (h-10)/3)), ink, bright*.4, false)
	}
}

func (c *diskCanvas) particles() {
	for i := 0; i < 180; i++ {
		x := float64((i*7919+127)%1400 + 20)
		y := float64((i*3571+43)%940 + 30)
		ink := c.cyan
		if i%3 == 0 {
			ink = c.violet
		}
		alpha := .14 + float64(i%5)*.05
		c.rect(x, y, 1.5, 1.5, ink, alpha)
		if i%17 == 0 {
			c.line(point{x - 3, y}, point{x + 3, y}, ink, .3, true)
			c.line(point{x, y - 3}, point{x, y + 3}, ink, .3, true)
		}
	}
}

func (c *diskCanvas) menuBar() {
	for i, label := range []string{"File", "Action", "View", "Help"} {
		x := 38 + float64(i)*125
		id := strings.ToLower(label)
		control := c.control("menu-"+id, nativeui.KindMenuItem, x, 18, 113, 44, label, c.d.state.Menu == id)
		if control.State.Selected || control.State.Hovered || control.State.Focused {
			c.recipe("menu-item", control)
		}
		c.glowText(x+15, 20, 94, 40, 27, label, mix(c.cyan, c.foreground, .8))
	}
	c.text(945, 24, 452, 27, 14, "LOCAL STORAGE / VOLUME INSPECTOR", c.muted)
	c.line(point{28, 72}, point{1412, 72}, c.cyan, .4, true)
}

func (c *diskCanvas) toolbar() {
	items := []struct {
		id, label string
		icon      nativeui.Icon
		ink       color.NRGBA
	}{
		{"previous", "Previous volume", nativeui.IconBack, c.cyan}, {"next", "Next volume", nativeui.IconForward, c.cyan},
		{"view", "Toggle usage and details", nativeui.IconSettings, c.cyan}, {"help", "Keyboard help", nativeui.IconSearch, c.violet},
		{"refresh", "Refresh sample disks", nativeui.IconPlay, c.cyan}, {"reset", "Reset sample view", nativeui.IconClose, c.d.selected.Color("danger")},
		{"grid", "Toggle perspective grid", nativeui.IconAdd, c.gold},
	}
	for i, item := range items {
		x := 39 + float64(i)*64
		selected := item.id == "view" && c.d.state.Details || item.id == "grid" && c.d.state.Grid || item.id == "help" && c.d.state.Help
		control := c.control(item.id, nativeui.KindIconButton, x, 91, 47, 44, item.label, selected)
		c.recipe("button", control)
		if p := c.painter(18); p != nil {
			c.check(p.DrawIcon(c.img, c.bounds(x+11, 102, 25, 23), item.icon, opaque(item.ink)))
		}
	}
	mode := "PARTITION MAP"
	if c.d.state.Details {
		mode = "USAGE / DETAILS"
	}
	c.text(553, 95, 375, 26, 17, mode, c.cyan)
	c.text(1030, 98, 365, 24, 14, fmt.Sprintf("SAMPLE %02d  /  1 DISK  /  %d VOLUMES", c.d.state.Sample+1, len(c.d.ordered())), c.muted)
}

var columns = []struct {
	id, label string
	x, w      float64
}{
	{"volume", "Volume", 36, 280}, {"layout", "Layout", 316, 110}, {"type", "Type", 426, 95}, {"filesystem", "File System", 521, 140},
	{"status", "Status", 661, 341}, {"capacity", "Capacity", 1002, 140}, {"free", "Free Space", 1142, 155}, {"percent", "% Free", 1297, 107},
}

func capacity(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.2f GB", mb/1024)
	}
	return fmt.Sprintf("%.0f MB", mb)
}
func (c *diskCanvas) tableBackdrop() {
	c.polygon(chamfer(26, 159, 1388, 246, 12), c.background, .96)
	c.frame(26, 159, 1388, 246, c.cyan, .88)
}

func (c *diskCanvas) table() {
	for _, column := range columns {
		label := column.label
		if c.d.state.Sort == column.id {
			if c.d.state.Descending {
				label += "  ▾"
			} else {
				label += "  ▴"
			}
		}
		control := c.control("sort-"+column.id, nativeui.KindButton, column.x, 169, column.w, 39, "Sort by "+column.label, c.d.state.Sort == column.id)
		if control.State.Selected || control.State.Hovered || control.State.Focused {
			c.recipe("button", control)
		}
		c.glowText(column.x+10, 173, column.w-16, 29, 18, label, mix(c.cyan, c.foreground, .6))
		if column.x > 36 {
			c.line(point{column.x, 165}, point{column.x, 355}, c.cyan, .22, false)
		}
	}
	for row, index := range c.d.ordered() {
		v := volumes[index]
		y := 214 + float64(row)*47
		control := c.control("row-"+v.id, nativeui.KindTableRow, 35, y, 1369, 43, v.name, c.d.state.Selected == v.id)
		if control.State.Selected || control.State.Hovered || control.State.Focused {
			c.recipe("table-row", control)
		}
		ink := c.volumeColor(index)
		c.rect(45, y+13, 14, 14, ink, .85)
		values := []string{v.name, "Simple", "Basic", v.filesystem, v.status, capacity(v.capacityMB), capacity(v.capacityMB - c.d.usedMB(index)), fmt.Sprintf("%.0f%%", 100*(1-c.d.usedMB(index)/v.capacityMB))}
		for col, column := range columns {
			x, width := column.x+10, column.w-17
			if col == 0 {
				x += 23
				width -= 23
			}
			text := mix(c.cyan, c.foreground, .76)
			if col == 4 {
				text = c.muted
			}
			c.text(x, y+6, width, 31, 17, values[col], text)
		}
		c.line(point{36, y + 44}, point{1404, y + 44}, c.cyan, .15, false)
	}
	if len(c.d.ordered()) == 0 {
		c.centered(40, 230, 1360, 87, 21, "No visible volumes — use the legend to restore a partition", c.muted)
	}
	c.text(45, 370, 950, 24, 14, "DISK 0  /  GPT  /  1863.00 GB  /  ONLINE", c.cyan)
	c.text(1110, 370, 286, 24, 13, "Click a column to sort", c.muted)
}

func (c *diskCanvas) volumeColor(index int) color.NRGBA {
	switch index {
	case 0:
		return c.cyan
	case 1:
		return c.gold
	default:
		return c.violet
	}
}

func (c *diskCanvas) partitionMap() {
	c.text(41, 435, 700, 33, 21, "DISK 0  /  PARTITION TOPOLOGY", c.cyan)
	// Soft pools of reflected light anchor the virtual geometry to the grid.
	for i, x := range []float64{345, 740, 1180} {
		if c.d.state.Visible[i] {
			c.floorLight(x, 808, []float64{145, 355, 185}[i], 38, c.volumeColor(i))
		}
	}
	if c.d.state.Grid {
		for i := -6; i <= 20; i++ {
			c.line(point{65 + float64(i)*105, 848}, point{590 + float64(i)*45, 497}, c.gridColor, .75, false)
		}
		for i := 0; i < 11; i++ {
			t := float64(i) / 10
			y := 503 + t*t*345
			c.line(point{35, y}, point{1404, y}, c.gridColor, .48, false)
		}
	}
	c.diskInfo()
	c.cube(0, 250, 590, 172, 220, 53, 69)
	c.cube(1, 480, 540, 516, 270, 70, 88)
	c.cube(2, 1055, 625, 244, 185, 57, 72)
	selected := "No partition selected"
	for i, v := range volumes {
		if v.id == c.d.state.Selected {
			selected = fmt.Sprintf("%s   /   %s   /   %s used   /   %s available", v.name, v.filesystem, capacity(c.d.usedMB(i)), capacity(v.capacityMB-c.d.usedMB(i)))
		}
	}
	c.text(50, 838, 1320, 30, 17, selected, c.foreground)
	c.line(point{39, 883}, point{1400, 883}, c.cyan, .35, true)
}

func (c *diskCanvas) floorLight(x, y, rx, ry float64, ink color.NRGBA) {
	bounds := c.bounds(x-rx, y-ry, rx*2, ry*2)
	for yy := bounds.Min.Y; yy < bounds.Max.Y; yy++ {
		for xx := bounds.Min.X; xx < bounds.Max.X; xx++ {
			u, v := (float64(xx)/c.sx-x)/rx, (float64(yy)/c.sy-y)/ry
			c.blend(xx, yy, ink, math.Exp(-3*(u*u+v*v))*.17)
		}
	}
	for i := 0; i < 16; i++ {
		px := x + (float64((i*53)%101)/100-.5)*rx*1.65
		py := y + (float64((i*71)%103)/100-.5)*ry*1.4
		c.line(point{px - 1, py}, point{px + 1, py}, ink, .7, true)
	}
}

func (c *diskCanvas) diskInfo() {
	x, y, w, h, dx, dy := float64(55), float64(620), float64(144), float64(190), float64(39), float64(53)
	top := []point{{x, y}, {x + dx, y - dy}, {x + w + dx, y - dy}, {x + w, y}}
	front := []point{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}
	side := []point{{x + w, y}, {x + w + dx, y - dy}, {x + w + dx, y + h - dy}, {x + w, y + h}}
	for _, face := range [][]point{side, top, front} {
		c.polygon(face, mix(c.background, c.cyan, .09), 1)
		c.outline(face, c.cyan, .85, true)
	}
	control := c.control("disk-info", nativeui.KindLabel, x, y-dy, w+dx, h+dy, "Disk 0, basic GPT disk, online, 1863.00 gigabytes", false)
	_ = control
	c.glowText(x+16, y+16, w-24, 35, 27, "Disk 0", c.cyan)
	c.text(x+16, y+62, w-24, 27, 18, "Basic / GPT", c.cyan)
	c.glowText(x+16, y+96, w-24, 27, 18, "1863.00 GB", c.cyan)
	c.rect(x+16, y+151, 8, 8, c.cyan, 1)
	c.text(x+31, y+139, w-39, 31, 18, "Online", c.cyan)
	c.line(point{x + 9, y - 9}, point{x + w + dx - 12, y - dy + 9}, c.cyan, .55, true)
}

func (c *diskCanvas) cube(index int, x, y, w, h, dx, dy float64) {
	v := volumes[index]
	ink := c.volumeColor(index)
	visible := c.d.state.Visible[index]
	front := []point{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}
	top := []point{{x, y}, {x + dx, y - dy}, {x + w + dx, y - dy}, {x + w, y}}
	side := []point{{x + w, y}, {x + w + dx, y - dy}, {x + w + dx, y + h - dy}, {x + w, y + h}}
	strength := 1.2
	var control nativeui.Control
	if visible {
		control = c.control("block-"+v.id, nativeui.KindButton, x, y-dy, w+dx, h+dy, v.name+" partition", c.d.state.Selected == v.id)
		if control.State.Selected || control.State.Focused || control.State.Hovered {
			strength = 1.75
		}
	} else {
		strength = .17
	}
	c.polygon(side, mix(c.background, ink, .025), 1)
	c.polygon(top, mix(c.background, ink, .055), 1)
	c.polygon(front, mix(c.background, ink, .027), 1)
	for _, face := range [][]point{side, top, front} {
		c.outline(face, ink, strength, true)
	}
	c.outline([]point{{x + 6, y + 6}, {x + w - 6, y + 6}, {x + w - 6, y + h - 6}, {x + 6, y + h - 6}}, ink, strength*.32, false)
	for i := 1; i < 9; i++ {
		t := float64(i) / 9
		c.line(point{x + w*t, y}, point{x + dx + w*t, y - dy}, ink, strength*.18, false)
	}
	for i := 1; i < 4; i++ {
		t := float64(i) / 4
		c.line(point{x + dx*t, y - dy*t}, point{x + w + dx*t, y - dy*t}, ink, strength*.16, false)
	}
	if !visible {
		c.centered(x+8, y+26, w-16, h-45, 18, "FILTERED", mix(c.background, ink, .45))
		return
	}
	name := []string{"EFI System", "C:", "Recovery"}[index]
	nameSize, nameHeight, capacityY, capacitySize, detailY := 25., 42., 66., 24., 117.
	if index == 1 {
		nameSize, nameHeight, capacityY, capacitySize, detailY = 46, 63, 104, 32, 174
	}
	c.glowText(x+17, y+18, w-29, nameHeight, nameSize, name, ink)
	amount := capacity(v.capacityMB)
	if index == 1 {
		amount += "  NTFS"
	}
	c.glowText(x+17, y+capacityY, w-29, 41, capacitySize, amount, ink)
	detail := []string{"System Partition", "Healthy (Primary Partition)", "Recovery Partition"}[index]
	if c.d.state.Details {
		detail = fmt.Sprintf("%.1f%% used", 100*c.d.usedMB(index)/v.capacityMB)
	}
	c.glowText(x+17, y+detailY, w-29, 28, 16, detail, ink)
	if index == 0 && !c.d.state.Details {
		c.text(x+17, y+151, w-29, 26, 16, "Healthy / FAT32", c.cyan)
	}
	barWidth := w - 32
	c.rect(x+16, y+h-23, barWidth, 5, ink, .18)
	c.rect(x+16, y+h-23, barWidth*c.d.usedMB(index)/v.capacityMB, 5, ink, .88)
	if control.State.Focused {
		c.frame(x-5, y-dy-5, w+dx+10, h+dy+10, ink, .6)
	}
	// A floating bracket follows the selected partition without enclosing the
	// whole map in a second fake window frame.
	if control.State.Selected {
		c.line(point{x + 12, y + h + 12}, point{x + w - 12, y + h + 12}, ink, 1, true)
		c.line(point{x + 12, y + h + 7}, point{x + 12, y + h + 16}, ink, .9, true)
		c.line(point{x + w - 12, y + h + 7}, point{x + w - 12, y + h + 16}, ink, .9, true)
	}
}

func (c *diskCanvas) legend() {
	for i, label := range []string{"EFI System", "Primary partition", "Recovery"} {
		x := 48 + float64(i)*304
		control := c.control("filter-"+volumes[i].id, nativeui.KindCheckbox, x, 912, 282, 43, label, c.d.state.Visible[i])
		c.recipe("button", control)
		ink := c.volumeColor(i)
		c.outline(chamfer(x+12, 923, 24, 19, 3), ink, 1.15, true)
		if c.d.state.Visible[i] {
			c.rect(x+16, 927, 16, 11, ink, .2)
		}
		c.text(x+48, 917, 223, 33, 19, label, c.foreground)
	}
	c.frame(1000, 912, 382, 43, c.violet, .55)
	c.text(1017, 917, 350, 32, 16, "DEMO / SAMPLE VOLUMES", c.muted)
	c.text(49, 969, 920, 23, 12, "TAB focus  ·  ENTER / SPACE activate  ·  ↑ ↓ select a volume  ·  ESC close menu", c.muted)
	c.text(1180, 969, 207, 23, 12, "TOPOLOGY / 01", c.cyan)
}

func (c *diskCanvas) popup() {
	if c.d.state.Menu != "" {
		x := float64(38)
		options := []struct{ id, label string }{{"reset", "Reset sample view"}, {"help", "About the fixture"}}
		switch c.d.state.Menu {
		case "action":
			x = 163
			options = []struct{ id, label string }{{"refresh", "Refresh sample disks"}, {"next", "Select next volume"}}
		case "view":
			x = 288
			options = []struct{ id, label string }{{"view", "Toggle usage / details"}, {"grid", "Toggle perspective grid"}}
		case "help":
			x = 413
			options = []struct{ id, label string }{{"help", "Keyboard shortcuts"}, {"reset", "Restore sample view"}}
		}
		c.polygon(chamfer(x, 65, 306, 113, 10), c.background, 1)
		c.frame(x, 65, 306, 113, c.cyan, .85)
		for i, option := range options {
			// Popup IDs are distinct from toolbar IDs; activation removes the
			// prefix before applying the shared action.
			id := "popup-" + option.id
			control := c.control(id, nativeui.KindMenuItem, x+9, 74+float64(i)*48, 288, 43, option.label, false)
			c.recipe("menu-item", control)
			c.text(x+21, 77+float64(i)*48, 264, 37, 18, option.label, c.foreground)
		}
	}
	if c.d.state.Help {
		c.rect(0, 0, initialWidth, initialHeight, c.background, .82)
		for i := range c.d.controls {
			c.d.controls[i].State.Disabled = true
		}
		c.polygon(chamfer(357, 485, 726, 285, 14), c.background, 1)
		c.frame(357, 485, 726, 285, c.violet, 1)
		c.text(387, 509, 669, 39, 26, "VOLUME INSPECTOR / HELP", c.cyan)
		for i, line := range []string{"Click a row or partition to select the same volume.", "Click a column heading to reverse its sort order.", "Legend switches filter both the table and partition map.", "Tab moves focus; Enter or Space activates a control.", "Arrow keys change the selected volume. Escape closes help.", "Refresh cycles sample values. No local disks are read or changed."} {
			c.text(387, 557+float64(i)*30, 660, 28, 17, line, c.foreground)
		}
		control := c.control("help-close", nativeui.KindButton, 1023, 496, 43, 39, "Close help", false)
		c.recipe("button", control)
		c.centered(1023, 496, 43, 39, 20, "×", c.cyan)
	}
}
