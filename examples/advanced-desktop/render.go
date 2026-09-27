package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
	xdraw "golang.org/x/image/draw"
)

type section struct{ name, title, caption, description string }

var sections = []section{
	{"Profile", "FORM / FUNCTION / EXPERIENCE", "THE ARCHITECTURE OF AN IDEA", "Worldr Advanced Studios is an independent interface laboratory. We build precise digital spaces where moving images, information and interaction share a single visual language."},
	{"Services", "IDEAS INTO ENVIRONMENTS", "STRATEGY / DESIGN / DEVELOPMENT", "From the initial sketch to the final interaction, our practice connects identity, spatial composition and software. Explore a coordinated approach to digital experiences."},
	{"Portfolio", "SELECTED WORK / 01—03", "SYSTEMS DESIGNED TO BE EXPLORED", "Browse three fictional projects below: an architectural archive, a motion study and a spatial instrument. Each project investigates a different relationship between form and behavior."},
	{"Accolades", "DETAIL MAKES THE DIFFERENCE", "RECOGNITION / SELECTED MILESTONES", "A collection of studio milestones celebrates clarity, craftsmanship and experimentation. These fictional project notes accompany this working interface demonstration."},
	{"Exploratory", "BEYOND THE EXPECTED", "RESEARCH / MOTION / PROTOTYPES", "An open-ended collection of motion tests, geometric studies and interactive experiments. The local demonstration reel traces a slow passage through the architectural image."},
	{"Transmissions", "SIGNAL / PROCESS / PROGRESS", "NOTES FROM THE STUDIO", "Follow design studies, production updates and selected technical notes. Filter the update archive or browse its entries with the navigation controls."},
	{"Contact", "LET'S BUILD SOMETHING", "CONTACT / COLLABORATION", "A conversation starts with an idea. The mailing-list form below demonstrates native text input and validation; submitted addresses remain local to this running application."},
}

type project struct{ title, category, detail string }

var projects = []project{
	{"STRUCTURAL STUDIES", "ARCHITECTURE / INTERACTIVE", "A visual archive of steel, light and negative space. The project studies a city through its structural connections."},
	{"MOTION SYSTEMS", "DIRECTION / MOTION", "A compact collection of moving compositions. Slow camera movement reveals the rhythm of repeating forms."},
	{"SPATIAL SIGNALS", "SOFTWARE / EXPERIMENTAL", "An instrument for organizing signals in space. Dense information remains accessible through coordinated controls."},
}

type update struct{ date, title, detail string }

var updates = []update{
	{"09.25.26", "Advanced interface released", "The working studio interface now includes native controls and live skins."},
	{"09.21.26", "Structural studies / volume 01", "New architectural compositions are available in the portfolio archive."},
	{"09.16.26", "Motion systems / process notes", "A thirty-second local demonstration reel explores scale and repetition."},
	{"09.08.26", "Spatial signals / prototype", "The latest instrument prototype focuses on clear, compact control layouts."},
	{"08.29.26", "Behind the grid / typography", "Small type, consistent spacing and fine rules provide a common structure."},
	{"08.18.26", "Production journal / materials", "Layered materials connect the window frame with the shared control system."},
	{"08.04.26", "Archive / summer collection", "Three fictional project collections are ready to explore."},
}

func (s *studio) setPainters(selected skin.Skin) error {
	compact := selected.Clone()
	scale := min(float64(s.width)/1200, float64(s.height)/960)
	compact.Metrics.Padding, compact.Metrics.Gap, compact.Metrics.ControlHeight = 4, 4, 24
	for name, recipe := range compact.Controls {
		recipe.ContentInsets = skin.Insets{Top: max(1, scale), Right: max(1, 4*scale), Bottom: max(1, scale), Left: max(1, 4*scale)}
		compact.Controls[name] = recipe
	}
	next := map[int]*nativeui.Painter{}
	for _, size := range []int{10, 11, 12, 13, 15, 18, 25, 32} {
		copy := compact.Clone()
		copy.Typography.Size, copy.Typography.LineHeight = max(6, float64(size)*scale), max(8, float64(size+3)*scale)
		theme, err := nativeui.ThemeFromSkin(copy)
		if err != nil {
			for _, p := range next {
				_ = p.Close()
			}
			return err
		}
		p, err := nativeui.NewPainter(theme)
		if err != nil {
			for _, p := range next {
				_ = p.Close()
			}
			return err
		}
		next[size] = p
	}
	for _, p := range s.painters {
		_ = p.Close()
	}
	s.painters = next
	return nil
}
func (s *studio) token(preferred, fallback string) color.RGBA {
	token := preferred
	if _, ok := s.selected.Palette[token]; !ok {
		token = fallback
	}
	return color.RGBAModel.Convert(s.selected.Color(token)).(color.RGBA)
}
func (s *studio) bounds(x, y, w, h int) image.Rectangle {
	sx, sy := float64(s.width)/1200, float64(s.height)/960
	return image.Rect(int(math.Round(float64(x)*sx)), int(math.Round(float64(y)*sy)), int(math.Round(float64(x+w)*sx)), int(math.Round(float64(y+h)*sy))).Intersect(image.Rect(0, 0, s.width, s.height))
}

type canvas struct {
	s                                                    *studio
	img                                                  *image.RGBA
	bg, panel, inset, edge, text, muted, heading, accent color.RGBA
	err                                                  error
}

func blend(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{R: uint8(float64(a.R)*(1-t) + float64(b.R)*t), G: uint8(float64(a.G)*(1-t) + float64(b.G)*t), B: uint8(float64(a.B)*(1-t) + float64(b.B)*t), A: uint8(float64(a.A)*(1-t) + float64(b.A)*t)}
}
func (c *canvas) rect(x, y, w, h int, ink color.Color) {
	draw.Draw(c.img, c.s.bounds(x, y, w, h), image.NewUniform(ink), image.Point{}, draw.Over)
}
func (c *canvas) border(x, y, w, h int, ink color.Color) {
	c.rect(x, y, w, 1, ink)
	c.rect(x, y+h-1, w, 1, ink)
	c.rect(x, y, 1, h, ink)
	c.rect(x+w-1, y, 1, h, ink)
}
func (c *canvas) gradient(x, y, w, h int, top, bottom color.RGBA) {
	for i := 0; i < h; i++ {
		c.rect(x, y+i, w, 1, blend(top, bottom, float64(i)/float64(max(1, h-1))))
	}
}
func (c *canvas) label(x, y, w, h, size int, text string, ink color.RGBA) {
	if c.err == nil {
		c.err = c.s.painters[size].DrawLabel(c.img, c.s.bounds(x, y, w, h), text, nativeui.LabelStyle{Color: ink})
	}
}
func (c *canvas) centered(x, y, w, h, size int, text string, ink color.RGBA) {
	if c.err == nil {
		c.err = c.s.painters[size].DrawLabel(c.img, c.s.bounds(x, y, w, h), text, nativeui.LabelStyle{Color: ink, Align: nativeui.AlignCenter})
	}
}
func (c *canvas) paragraph(x, y, w, size, lineH, maxLines int, text string, ink color.RGBA) {
	words, line, n := strings.Fields(text), "", 0
	limit := max(8, int(float64(w)/(float64(size)*.63)))
	for _, word := range words {
		if len(line)+len(word)+1 > limit && line != "" {
			c.label(x, y+n*lineH, w, lineH, size, line, ink)
			n++
			line = ""
			if n == maxLines {
				return
			}
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" && n < maxLines {
		c.label(x, y+n*lineH, w, lineH, size, line, ink)
	}
}
func (c *canvas) control(id string, kind nativeui.Kind, x, y, w, h int, label string, state nativeui.State) nativeui.Control {
	control := nativeui.Control{ID: id, Kind: kind, Bounds: c.s.bounds(x, y, w, h), Label: label, State: state}
	if !control.Bounds.Empty() {
		c.s.controls = append(c.s.controls, control)
	}
	return c.s.controller.Decorate(control)
}
func (c *canvas) button(id string, x, y, w, h int, label string, selected bool) {
	control := c.control(id, nativeui.KindButton, x, y, w, h, label, nativeui.State{Selected: selected})
	if !control.Bounds.Empty() && c.err == nil {
		c.err = c.s.painters[11].DrawButton(c.img, control)
	}
}
func (c *canvas) tab(id string, x, y, w, h int, label string, selected bool) {
	control := c.control(id, nativeui.KindTab, x, y, w, h, label, nativeui.State{Selected: selected})
	if !control.Bounds.Empty() && c.err == nil {
		c.err = c.s.painters[11].DrawButton(c.img, control)
	}
}
func (c *canvas) panelHeader(x, y, w int, title, index string) {
	c.gradient(x, y, w, 26, blend(c.edge, c.panel, .72), c.panel)
	c.border(x, y, w, 26, c.edge)
	c.rect(x+7, y+9, 5, 7, c.accent)
	c.label(x+19, y+3, w-75, 20, 12, title, c.heading)
	c.label(x+w-48, y+3, 42, 20, 10, index, c.muted)
	c.rect(x, y+29, w, 1, c.edge)
}
func (c *canvas) field(id string, x, y, w, h int, label, placeholder string, e editor, invalid bool) {
	control := c.control(id, nativeui.KindField, x, y, w, h, label, nativeui.State{Invalid: invalid})
	if control.Bounds.Empty() {
		return
	}
	// Publish the value separately from its accessible label.
	c.s.controls[len(c.s.controls)-1].Text = e.Text
	p := c.s.painters[11]
	if err := p.DrawControlBackground(c.img, "field", control.Bounds, control.State); err != nil {
		c.err = err
		return
	}
	inner := p.ControlContentBounds("field", control.Bounds)
	if inner.Empty() {
		return
	}
	buffer := image.NewRGBA(image.Rect(0, 0, 4096, inner.Dy()))
	measure := func(text string) int {
		m := image.NewRGBA(image.Rect(0, 0, 4096, inner.Dy()))
		_ = p.DrawLabel(m, m.Bounds(), text+"|", nativeui.LabelStyle{Color: c.text})
		for xx := m.Bounds().Dx() - 1; xx >= 0; xx-- {
			for yy := 0; yy < m.Bounds().Dy(); yy++ {
				if m.RGBAAt(xx, yy).A != 0 {
					return max(0, xx-1)
				}
			}
		}
		return 0
	}
	value, ink := e.Text, c.text
	if value == "" && e.Preedit == "" {
		value, ink = placeholder, c.muted
	}
	if e.Preedit != "" {
		value = e.Text[:e.Cursor] + e.Preedit + e.Text[e.Cursor:]
	}
	cursor := measure(e.Text[:e.Cursor] + e.Preedit)
	if control.State.Focused && e.Anchor != e.Cursor {
		lo, hi := measure(e.Text[:min(e.Anchor, e.Cursor)]), measure(e.Text[:max(e.Anchor, e.Cursor)])
		draw.Draw(buffer, image.Rect(lo, 1, hi, inner.Dy()-1), image.NewUniform(c.s.token("selection", "raised")), image.Point{}, draw.Src)
	}
	if err := p.DrawLabel(buffer, buffer.Bounds(), value, nativeui.LabelStyle{Color: ink}); err != nil {
		c.err = err
		return
	}
	if control.State.Focused {
		draw.Draw(buffer, image.Rect(cursor, 2, cursor+1, max(2, inner.Dy()-2)), image.NewUniform(c.accent), image.Point{}, draw.Over)
	}
	if e.Preedit != "" {
		draw.Draw(buffer, image.Rect(measure(e.Text[:e.Cursor]), max(0, inner.Dy()-2), cursor, inner.Dy()-1), image.NewUniform(c.accent), image.Point{}, draw.Over)
	}
	offset := max(0, cursor-inner.Dx()+8)
	draw.Draw(c.img, inner, buffer, image.Pt(offset, 0), draw.Over)
}

// Each of the two photograph placements retains only its latest exact crop.
// The immutable source is decoded once at Start; a fractional reel advance
// reuses its result until the integer source crop actually moves.
type cachedPhotograph struct {
	source image.Rectangle
	pixels *image.RGBA
}

func (c *canvas) photograph(slot, x, y, w, h int, phase float64) {
	target := c.s.bounds(x, y, w, h)
	if target.Empty() {
		return
	}
	source := c.s.architecture.Bounds()
	aspect := float64(target.Dx()) / float64(target.Dy())
	if float64(source.Dx())/float64(source.Dy()) > aspect {
		width := int(float64(source.Dy()) * aspect)
		source.Min.X += int(float64(source.Dx()-width) * (.35 + .3*phase))
		source.Max.X = source.Min.X + width
	} else {
		height := int(float64(source.Dx()) / aspect)
		source.Min.Y += int(float64(source.Dy()-height) * (.35 + .3*phase))
		source.Max.Y = source.Min.Y + height
	}
	cache := &c.s.photographs[slot]
	if cache.pixels == nil || cache.pixels.Bounds().Size() != target.Size() || cache.source != source {
		cache.pixels = image.NewRGBA(image.Rectangle{Max: target.Size()})
		cache.source = source
		xdraw.CatmullRom.Scale(cache.pixels, cache.pixels.Bounds(), c.s.architecture, source, xdraw.Src, nil)
	}
	draw.Draw(c.img, target, cache.pixels, image.Point{}, draw.Over)
}
func (c *canvas) diagram(x, y, w, h int, variant int) {
	c.rect(x, y, w, h, c.inset)
	c.border(x, y, w, h, c.edge)
	for i := 0; i < 8; i++ {
		xx := x + 7 + i*(w-14)/8
		c.rect(xx, y+4, 1, h-8, blend(c.inset, c.edge, .25))
	}
	for i := 0; i < 4; i++ {
		yy := y + 8 + i*(h-16)/4
		c.rect(x+4, yy, w-8, 1, blend(c.inset, c.edge, .25))
	}
	for i := 0; i < 12; i++ {
		height := int(float64(h-14) * (.2 + .7*math.Abs(math.Sin(float64(i)*.7+float64(variant)))))
		c.gradient(x+8+i*(w-16)/12, y+h-height-6, max(2, (w-18)/12-3), height, c.accent, c.panel)
	}
}

func (s *studio) paint() error {
	if s.closed {
		return nil
	}
	img := s.pixels
	if img == nil || img.Bounds().Size() != image.Pt(s.width, s.height) {
		img = image.NewRGBA(image.Rect(0, 0, s.width, s.height))
	}
	s.controls = s.controls[:0]
	c := &canvas{s: s, img: img, bg: s.token("page-background", "background"), panel: s.token("page-panel", "surface"), inset: s.token("page-inset", "background"), edge: s.token("page-edge", "border"), text: s.token("page-text", "text"), muted: s.token("page-muted", "muted"), heading: s.token("heading-text", "text"), accent: s.token("accent", "text")}
	draw.Draw(img, img.Bounds(), image.NewUniform(c.bg), image.Point{}, draw.Src)
	c.border(34, 0, 1132, 883, c.edge)
	c.border(38, 4, 1124, 875, c.inset)
	// Narrow dark hardware strip and a broad brushed-metal masthead.
	c.gradient(40, 6, 1120, 47, c.inset, c.panel)
	c.label(52, 14, 435, 20, 10, "W O R L D R   /   I N T E R A C T I V E   S Y S T E M S", c.muted)
	c.label(838, 14, 308, 20, 10, "INDEX.07    SIGNAL: ONLINE    [ + ]", c.text)
	for i := 0; i < 22; i++ {
		c.rect(52+i*8, 43, 3, 3, c.edge)
	}
	c.gradient(40, 56, 1120, 79, blend(c.edge, c.panel, .55), c.panel)
	c.border(40, 56, 1120, 79, c.edge)
	// The angular WR mark is vector geometry, not baked-in image text.
	for i := 0; i < 38; i++ {
		c.rect(63+i/2, 75+i, 7, 1, c.heading)
		c.rect(102-i/2, 75+i, 7, 1, c.heading)
		c.rect(100+i/2, 75+i, 7, 1, c.heading)
		c.rect(139-i/2, 75+i, 7, 1, c.heading)
	}
	c.label(160, 66, 540, 39, 32, "WORLD R", c.heading)
	c.label(164, 102, 560, 22, 13, "A D V A N C E D   S T U D I O S", c.text)
	c.label(816, 76, 319, 21, 12, "FORM FOLLOWS INTERACTION", c.heading)
	c.label(817, 98, 306, 16, 10, "Independent design / motion / software", c.muted)
	for i, item := range sections {
		c.tab(fmt.Sprintf("nav-%d", i), 40+i*160, 143, 159, 26, strings.ToUpper(item.name), s.state.Section == i)
	}
	c.border(40, 179, 1120, 323, c.edge)
	c.photograph(0, 43, 182, 1114, 317, s.state.Reel)
	// Sparse technical notation sits directly over the image; no headline plate
	// competes with the crossing architectural forms.
	section := sections[s.state.Section]
	grid := color.NRGBA{R: c.accent.R, G: c.accent.G, B: c.accent.B, A: 24}
	for x := 814; x < 1150; x += 28 {
		c.rect(x, 195, 1, 292, grid)
	}
	for y := 195; y < 490; y += 24 {
		c.rect(814, y, 335, 1, grid)
	}
	c.label(62, 196, 515, 15, 10, "STRUCTURAL ARCHIVE / 07     OPTICAL STUDY / A.03", c.heading)
	c.label(816, 263, 330, 43, 32, "WORLD R", c.heading)
	c.label(818, 305, 325, 23, 15, strings.ToUpper(section.name)+" / ADVANCED STUDIOS", c.heading)
	c.label(818, 333, 325, 18, 10, section.caption, c.accent)
	c.label(818, 355, 325, 17, 10, "DIGITAL EXPERIENCE / INDEPENDENT IDEAS", c.muted)
	for _, target := range [][3]int{{1098, 440, 25}, {1031, 454, 12}, {1137, 466, 7}} {
		x, y, r := target[0], target[1], target[2]
		ink := color.NRGBA{R: c.accent.R, G: c.accent.G, B: c.accent.B, A: 82}
		for yy := -r; yy <= r; yy++ {
			for xx := -r; xx <= r; xx++ {
				distance := xx*xx + yy*yy
				if distance < r*r && distance >= (r-1)*(r-1) {
					c.rect(x+xx, y+yy, 1, 1, ink)
				}
			}
		}
		c.rect(x-r-5, y, r*2+10, 1, ink)
		c.rect(x, y-r-5, 1, r*2+10, ink)
	}
	c.gradient(40, 507, 1120, 25, c.panel, c.inset)
	c.border(40, 507, 1120, 25, c.edge)
	c.label(48, 511, 897, 17, 10, s.state.Status, c.text)
	c.label(991, 510, 150, 19, 10, fmt.Sprintf("REEL 00:%02d / 00:30", int(s.state.Reel*30)), c.muted)
	c.rect(40, 531, int(1120*s.state.Reel), 2, c.accent)
	c.feature()
	c.subdata()
	c.updatePanel()
	c.footer()
	c.rect(40, 844, 1120, 1, c.edge)
	c.label(48, 854, 754, 18, 10, "COPYRIGHT 2026 WORLDR  /  ADVANCED STUDIOS  /  INTERFACE DEMONSTRATION", c.muted)
	c.label(916, 854, 236, 18, 10, "LOCAL EXPERIENCE  /  V.01", c.muted)
	for i := 0; i < 9; i++ {
		c.rect(40+i*126, 890, 100, 1, blend(c.bg, c.edge, .32))
	}
	if c.err != nil {
		return c.err
	}
	if err := s.controller.SetControlsWithin(s.controls, img.Bounds()); err != nil {
		return err
	}
	s.pixels, s.dirty = img, true
	return nil
}
func (c *canvas) feature() {
	c.panelHeader(40, 547, 392, "FEATURE / SELECTED WORK", "01.03")
	project := projects[c.s.state.Portfolio]
	c.photograph(1, 43, 585, 120, 91, float64(c.s.state.Portfolio)/3)
	c.border(42, 584, 122, 93, c.edge)
	c.label(176, 583, 251, 19, 13, project.title, c.heading)
	c.label(176, 604, 251, 16, 10, project.category, c.accent)
	c.paragraph(176, 625, 249, 11, 15, 4, project.detail, c.text)
	for i := range projects {
		c.button(fmt.Sprintf("portfolio-%d", i), 43+i*130, 689, 126, 25, fmt.Sprintf("PROJECT / %02d", i+1), c.s.state.Portfolio == i)
	}
}
func (c *canvas) subdata() {
	c.panelHeader(443, 547, 288, "SUB.DATA / STUDIO", "ONLINE")
	c.paragraph(450, 583, 274, 11, 15, 4, sections[c.s.state.Section].description, c.text)
	c.diagram(450, 648, 103, 33, c.s.state.Section)
	c.label(565, 648, 157, 16, 10, "DIGITAL EXPERIENCE", c.heading)
	c.label(565, 665, 157, 15, 10, "AUDIO / VISUAL / CODE", c.muted)
	play := "PLAY DEMO REEL"
	if c.s.state.Playing {
		play = "PAUSE DEMO REEL"
	}
	c.button("play", 450, 689, 205, 25, play, c.s.state.Playing)
	c.button("reel-reset", 663, 689, 60, 25, "RESET", false)
}
func (c *canvas) updatePanel() {
	c.panelHeader(742, 547, 418, "UPDATES / TRANSMISSIONS", "INDEX")
	c.field("filter", 750, 581, 350, 25, "Filter updates", "Filter the archive…", c.s.state.Filter, false)
	c.button("updates-prev", 1106, 581, 21, 25, "<", false)
	c.button("updates-next", 1132, 581, 21, 25, ">", false)
	matches := c.s.filteredUpdates()
	offset := min(c.s.state.UpdateOffset, max(0, len(matches)-3))
	for row := 0; row < 3 && offset+row < len(matches); row++ {
		index := matches[offset+row]
		item := updates[index]
		y := 613 + row*32
		control := c.control(fmt.Sprintf("update-%d", index), nativeui.KindListRow, 750, y, 403, 30, item.title, nativeui.State{Selected: c.s.state.SelectedUpdate == index})
		if err := c.s.painters[11].DrawControlBackground(c.img, "list-row", control.Bounds, control.State); err != nil {
			c.err = err
		}
		c.label(758, y+2, 86, 22, 10, item.date, c.muted)
		c.label(850, y+2, 290, 22, 11, item.title, c.text)
	}
	if len(matches) == 0 {
		c.label(756, 624, 390, 23, 11, "No transmissions match this filter.", c.muted)
	}
}
func (c *canvas) footer() {
	c.panelHeader(40, 735, 392, "MAILING LIST / STAY INFORMED", "LOCAL")
	c.label(47, 772, 374, 17, 10, "Studio notes and selected project announcements.", c.muted)
	c.field("email", 47, 794, 272, 26, "Email address", "Your email address", c.s.state.Email, c.s.state.InvalidEmail)
	c.button("subscribe", 326, 794, 98, 26, "SUBSCRIBE", c.s.state.Subscribed)
	status, ink := "LOCAL DEMO / NO ADDRESS IS SENT", c.muted
	if c.s.state.Subscribed {
		status, ink = "SAVED / THANK YOU FOR YOUR INTEREST", c.s.token("success", "accent")
	}
	if c.s.state.InvalidEmail {
		status, ink = "PLEASE ENTER A VALID EMAIL ADDRESS", c.s.token("danger", "accent")
	}
	c.label(48, 824, 376, 15, 10, status, ink)
	c.panelHeader(443, 735, 288, "EQUIPMENT / CAPABILITIES", "V.01")
	for i, pair := range [][2]string{{"RENDER", "Native surfaces / live skins"}, {"CONTROL", "Pointer / keyboard / IME"}, {"MEDIA", "Image / vector / typography"}} {
		y := 776 + i*20
		c.label(451, y, 62, 18, 10, pair[0], c.accent)
		c.label(517, y, 207, 18, 10, pair[1], c.text)
	}
	c.panelHeader(742, 735, 418, "TRANSMISSIONS / CHANNEL STATUS", "07")
	c.label(752, 775, 396, 18, 11, "Independent ideas. Connected experiences.", c.text)
	control := c.control("transmissions", nativeui.KindCheckbox, 752, 799, 397, 22, "Enable local transmission channel", nativeui.State{Selected: c.s.state.Transmissions})
	if !control.Bounds.Empty() {
		if err := c.s.painters[11].DrawCheckbox(c.img, control); err != nil {
			c.err = err
		}
	}
	channelStatus := "CHANNEL / ACTIVE"
	if !c.s.state.Transmissions {
		channelStatus = "CHANNEL / STANDBY"
	}
	c.label(753, 825, 394, 15, 10, channelStatus, c.muted)
}
