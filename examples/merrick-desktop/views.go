package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"time"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
	xdraw "golang.org/x/image/draw"
)

func (c *canvas) calendar() {
	w, h := c.v.spec.width, c.v.spec.height
	c.rect(w-25, 0, 25, h-22, c.p.blue)
	c.bevel(23, 0, 107, 20, c.p.blue)
	c.text(28, 1, 42, 17, 10, "File", c.p.onDark)
	c.text(72, 1, 44, 17, 10, "Edit", c.p.onDark)
	start := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC).AddDate(0, 0, c.d.state.Week*7)
	c.text(26, 22, 132, 20, 11, start.Format("January 2006"), c.p.ink)
	c.center(w-24, 30, 24, 16, 9, "wk", c.p.onDark)
	c.center(w-24, 47, 24, 22, 18, fmt.Sprintf("%02d", 39+c.d.state.Week), c.p.onDark)
	c.button("week-prev", w-23, 83, 21, 20, "<", false)
	c.button("week-next", w-23, 108, 21, 20, ">", false)
	events := [][]string{{"09  Whitman briefing", "11  Research induction", "14  Delta review"}, {"09  Alpha review", "11  Central systems", "15  Archive scan"}, {"09  Echo review", "13  Pattern analysis"}, {"09  Paper submission", "12  Bravo review"}, {"10  Board presentation", "13  Charlie review", "16  Interface study"}, {"11  Lab progress", "15  Weekly summary"}}
	for day := 0; day < 6; day++ {
		y := 47 + day*105
		date := start.AddDate(0, 0, day)
		selected := c.d.state.Day == day
		control := c.control(fmt.Sprintf("day-%d", day), nativeui.KindButton, 24, y, 137, 100, date.Format("Monday January 2"), nativeui.State{Selected: selected}, 0)
		fill := c.p.panel
		if selected || control.State.Hovered {
			fill = rgb(0xe6e8ee)
			if c.d.selected.ID != "merrick" {
				fill = c.p.paper
			}
		}
		c.rect(24, y, 137, 100, fill)
		c.bevel(24, y, 137, 17, c.p.blue)
		c.text(28, y, 24, 17, 12, date.Format("02"), c.p.onDark)
		c.text(53, y+1, 90, 15, 9, strings.ToUpper(date.Format("Monday")), c.p.onDark)
		c.circle(148, y+8, 3, c.p.onDark, false)
		for i, event := range events[day] {
			yy := y + 23 + i*21
			c.bevel(30, yy, 125, 19, c.p.navy)
			c.rect(49, yy+1, 1, 17, c.p.edge)
			c.text(33, yy+1, 17, 15, 9, event[:2], c.p.onDark)
			c.text(54, yy+1, 98, 15, 9, event[4:], c.p.onDark)
		}
		if selected || control.State.Focused {
			c.border(24, y, 137, 100, c.p.blue)
			c.rect(24, y+17, 3, 83, c.p.blue)
		}
	}
	c.footer("New", "Com", "Week")
	c.text(26, h-44, 130, 17, 9, "LOCAL / 6 DAY VIEW", c.p.muted)
}

var nutrientNames = [...]string{"Vitamin A", "Beta-carotene", "Vitamin D", "Vitamin E", "Vitamin K", "Vitamin C"}

func (c *canvas) records() {
	w, h := c.v.spec.width, c.v.spec.height
	columns := []struct {
		x, w  int
		title string
	}{{22, 208, "Profile"}, {234, 204, "Research Record"}, {442, 224, "Visual Archive"}, {670, 216, "Nutrition"}}
	for _, col := range columns {
		c.rect(col.x, 23, col.w, h-45, c.p.panel)
		c.bevel(col.x, 0, col.w, 19, c.p.blue)
		c.text(col.x+6, 0, col.w-12, 17, 9, "File     Edit      Format", c.p.onDark)
		c.rect(col.x, 23, col.w, 20, c.p.blue)
		c.text(col.x+5, 24, col.w-11, 17, 11, col.title, c.p.onDark)
		c.border(col.x, 43, col.w, h-66, c.p.navy)
	}
	x := 28
	c.text(x, 48, 178, 23, 14, "Jordan Two Delta", c.p.ink)
	rows := [][2]string{{"CODE", "1022/4516/GHJ"}, {"PROJECT", "MERRICK / 07"}, {"ACCESS", "Research observer"}, {"SECTOR", "Olive / Level 04"}, {"CYCLE", "24 cycles / 1 review"}, {"ARCHIVE", "Central collection"}}
	for i, row := range rows {
		y := 77 + i*24
		c.line(x, y, 211, y, c.p.muted)
		c.text(x+2, y+2, 48, 16, 9, row[0], c.p.muted)
		c.text(x+51, y+2, 129, 18, 10, row[1], c.p.ink)
	}
	c.text(x+5, 226, 173, 16, 10, "Assigned researcher", c.p.muted)
	c.text(x+5, 240, 172, 20, 12, "Sarah Jordan", c.p.ink)
	c.check("access", x+5, 266, 169, 18, "Archive access", c.d.state.Access)
	c.rect(214, 45, 13, 249, c.p.navy)
	c.vertical(215, 50, 12, 177, "Agnate profile / identity", c.p.onDark)
	for i, label := range []string{"Status", "History", "Notes"} {
		c.button(fmt.Sprintf("record-tab-%d", i), 240+i*61, 48, 59, 18, label, c.d.state.RecordTab == i)
	}
	if c.d.state.RecordTab == 0 {
		c.text(244, 73, 163, 18, 12, "Research clearance", c.p.ink)
		checks := []string{"Identity verified", "Archive indexed", "Pattern review", "Training complete", "Observer assigned", "Sample catalogued"}
		for i, label := range checks {
			y := 99 + i*20
			c.text(244, y, 144, 16, 10, label, c.p.ink)
			c.border(408, y+3, 7, 7, c.p.muted)
			if i != 2 {
				c.line(410, y+6, 412, y+8, c.p.ink)
				c.line(412, y+8, 417, y+2, c.p.ink)
			}
		}
		c.line(244, 226, 420, 226, c.p.muted)
		c.text(244, 231, 174, 16, 11, "Screening / review complete", c.p.ink)
		for i := 0; i < 12; i++ {
			c.circle(255+i*12, 268, 8, c.p.blue, false)
			if i%3 == 0 {
				c.circle(255+i*12, 268, 3, c.p.navy, true)
			}
		}
	} else if c.d.state.RecordTab == 1 {
		c.text(244, 75, 171, 18, 12, "Revision history", c.p.ink)
		for i, row := range []string{"08:12  Access approved", "09:04  Archive synchronized", "10:28  Observer note added", "11:10  Record reviewed", "12:06  Collection verified", "13:40  Summary released"} {
			c.text(244, 103+i*24, 174, 20, 10, row, c.p.ink)
			c.line(244, 123+i*24, 421, 123+i*24, c.p.edge)
		}
	} else {
		c.text(244, 75, 171, 18, 12, "Project notes", c.p.ink)
		c.paragraph(244, 103, 172, 10, "This fictional profile belongs to the Merrick interface study. The team is reviewing archival continuity, annotation quality and access patterns across the collection.", 11)
	}
	c.rect(424, 45, 12, 249, c.p.navy)
	c.vertical(425, 51, 11, 194, "Class 1 / Class 2 / DNA profile", c.p.onDark)
	portrait := image.Rect(449, 48, 649, 262)
	if c.d.portrait != nil {
		c.portrait(portrait)
	} else {
		c.portraitSchematic(portrait)
	}
	c.text(451, 263, 190, 15, 9, "VISUAL ID  /  OBSERVER  02", c.p.muted)
	c.check("archive-sync", 450, 279, 196, 16, "Archive synchronized", c.d.state.ArchiveSync)
	c.rect(650, 45, 13, 249, c.p.navy)
	c.vertical(651, 51, 12, 190, "Visual archive / capture 02", c.p.onDark)
	for i, name := range nutrientNames {
		y := 50 + i*35
		c.text(678, y, 119, 14, 9, name, c.p.ink)
		c.text(804, y, 60, 14, 9, fmt.Sprintf("%02d  %.0f", i+1, c.d.state.Nutrients[i]), c.p.muted)
		c.slider(fmt.Sprintf("nutrient-%d", i), 678, y+14, 190, 17, c.d.state.Nutrients[i])
	}
	c.rect(678, 267, 192, 1, c.p.edge)
	c.text(679, 271, 188, 17, 9, "FICTIONAL PROFILE / 6 CHANNELS", c.p.muted)
	c.footer("Set", "Note", "Flag")
	c.text(242, h-22, 165, 17, 9, "Sel     Res.     Add", c.p.onDark)
	c.text(450, h-22, 191, 17, 9, "New    Tag     Archive", c.p.onDark)
	c.text(w-208, h-22, 188, 17, 9, "Add     Del     Compare", c.p.onDark)
}

func (c *canvas) portrait(bounds image.Rectangle) {
	target := c.r(bounds.Min.X, bounds.Min.Y, bounds.Dx(), bounds.Dy())
	if target.Empty() {
		return
	}
	if cache := c.d.portraitCache; cache != nil && cache.Bounds().Size() == target.Size() {
		draw.Draw(c.image, target, cache, image.Point{}, draw.Src)
		return
	}
	source := c.d.portrait.Bounds()
	aspect := float64(target.Dx()) / float64(max(1, target.Dy()))
	if float64(source.Dx())/float64(source.Dy()) > aspect {
		width := int(float64(source.Dy()) * aspect)
		source.Min.X += (source.Dx() - width) / 2
		source.Max.X = source.Min.X + width
	} else {
		height := int(float64(source.Dx()) / aspect)
		source.Min.Y += (source.Dy() - height) / 2
		source.Max.Y = source.Min.Y + height
	}
	xdraw.CatmullRom.Scale(c.image, target, c.d.portrait, source, xdraw.Over, nil)
	// Retain the composited patch, preserving Catmull-Rom's exact rounding for
	// custom portraits with alpha. Its panel background changes only on skin.
	cache := image.NewRGBA(image.Rectangle{Max: target.Size()})
	draw.Draw(cache, cache.Bounds(), c.image, target.Min, draw.Src)
	c.d.portraitCache = cache
}
func (c *canvas) portraitSchematic(bounds image.Rectangle) {
	x, y, w, h := bounds.Min.X, bounds.Min.Y, bounds.Dx(), bounds.Dy()
	c.rect(x, y, w, h, rgb(0xb8bcc5))
	c.rect(x+1, y+1, w-2, h-2, rgb(0xdce0e5))
	cx := x + w/2
	cy := y + h/2 - 28
	for yy := 0; yy < h; yy += 12 {
		c.line(x+1, y+yy, x+w-2, y+yy, rgb(0xc8cdd5))
	}
	for xx := 0; xx < w; xx += 12 {
		c.line(x+xx, y+1, x+xx, y+h-2, rgb(0xc8cdd5))
	}
	c.circle(cx, cy, 51, rgb(0xa0a8b4), true)
	c.circle(cx, cy+4, 43, rgb(0xc1c6cd), true)
	c.rect(cx-17, cy+39, 34, 40, rgb(0xaab2bd))
	for yy := 0; yy < 65; yy++ {
		span := min(w/2-14, 24+yy)
		c.rect(cx-span, cy+69+yy, span*2, 1, rgb(0x6b7c8e))
	}
	c.line(cx-30, cy-5, cx-11, cy-8, rgb(0x657384))
	c.line(cx+11, cy-8, cx+30, cy-5, rgb(0x657384))
	c.line(cx, cy-1, cx-4, cy+19, rgb(0x8c98a5))
	c.line(cx-13, cy+29, cx+13, cy+29, rgb(0x8c98a5))
	c.border(x+10, y+10, w-20, h-20, rgb(0x8b97a7))
	c.text(x+14, y+h-24, w-28, 16, 9, "ARCHIVE / PORTRAIT SCHEMATIC", rgb(0xf2f4f6))
}

var documentTitles = [][]string{
	{"Research Methods", "Collection Protocol", "Review Summary"},
	{"Habitat Study", "Spatial Observation", "Interface Notes"},
	{"Signal Reconstruction", "Pattern Catalogue", "Archive Continuity"},
}
var documentParagraphs = [][]string{
	{"The archive is a record of observation. Each entry preserves a source, a time and a short account of the circumstances in which the material was collected. Together these fragments describe the evolution of a working research environment.", "This study compares three arrangements of the same information: a continuous page, a structured profile and a compact calendar. Participants locate an event, compare two records and return to the original document without losing their place.", "The resulting notes distinguish the appearance of an interface from the work it supports. Typography, alignment and durable visual landmarks provide continuity while windows move through the workspace.", "All names, records and measurements in this collection are fictional material prepared for the Merrick desktop demonstration."},
	{"A spatial workspace permits several documents to remain visible at once. Their arrangement records an intention: one page supplies context, a second contains a developing argument and a third holds the original observation.", "The team placed a narrow schedule beside a wider profile, leaving an open field for documents. Compact program and message palettes remain reachable at the edge. The arrangement is revisited at the beginning of each review cycle.", "Window movement should preserve reading position and selection. An appearance change should retain the same application data, controls and document history. These behaviors make the space predictable over repeated sessions.", "The current study contains three pages. Use the page controls below to compare the associated project notes."},
	{"The signal archive preserves intermediate results alongside final interpretations. A clear source history allows a researcher to distinguish measured values, annotations and generated illustrations at a glance.", "The diagrams at the right are synthetic pattern samples. Their concentric forms visualize a changing field and are used only to exercise the composition of text, imagery and small interface controls.", "A useful archive offers several routes through its material. The calendar follows time, the profile groups related records and the document view provides uninterrupted reading. Each route shares the same visual language.", "This page is part of a fictional interface collection. It contains no clinical records or assessment."},
}

func (c *canvas) document() {
	w, h := c.v.spec.width, c.v.spec.height
	doc := int(c.v.spec.id) - 3
	page := c.d.state.Pages[doc]
	c.rect(0, 0, w, 24, c.p.navy)
	c.bevel(2, 1, 98, 18, c.p.blue)
	c.text(7, 2, 87, 16, 9, "Edit      History", c.p.onDark)
	c.text(128, 2, 200, 16, 9, "Save     View     Print    +  -", c.p.onDark)
	c.rect(8, 28, w-16, h-55, rgb(0xf5f5f2))
	c.border(12, 32, w-24, h-63, rgb(0x7b7b7b))
	c.text(23, 37, w-107, 15, 9, "I N T E R N A L   P A P E R", rgb(0x232323))
	c.text(w-92, 38, 70, 16, 9, "M E R R I C K", rgb(0x676767))
	c.line(21, 56, w-21, 56, rgb(0x777777))
	c.text(23, 58, 198, 12, 9, "UNIT / RESEARCH        REF / 07-"+fmt.Sprintf("%03d", doc*3+page+1), rgb(0x5a5a5a))
	c.line(21, 73, w-21, 73, rgb(0x777777))
	c.text(23, 75, 240, 12, 9, "PROJECT ARCHIVE  /  FICTIONAL RECORD", rgb(0x9b5858))
	c.line(21, 91, w-21, 91, rgb(0x777777))
	c.text(25, 101, w-50, 22, 12, documentTitles[doc][page], rgb(0x1e1e1e))
	if doc != 2 {
		c.watermark(236, 280, doc+page)
	}
	oldInk := c.p.ink
	c.p.ink = rgb(0x292929)
	y := 132
	textWidth := w - 53
	if doc == 2 {
		textWidth = w - 119
	}
	for i := 0; i < 3; i++ {
		index := (i + page) % len(documentParagraphs[doc])
		remaining := min(7, max(0, (h-85-y)/14))
		if remaining == 0 {
			break
		}
		y = c.paragraph(25, y, textWidth, 10, documentParagraphs[doc][index], remaining) + 10
		if y > h-91 {
			break
		}
	}
	c.p.ink = oldInk
	if doc == 2 {
		for i := 0; i < 3; i++ {
			c.syntheticScan(w-65, 147+i*72, 43, 57, float64(i+page)*.7)
		}
	}
	c.text(26, h-66, w-90, 14, 9, "MERRICK RESEARCH / COLLECTION 07", rgb(0x888888))
	c.text(w-57, h-67, 35, 14, 10, fmt.Sprintf("%05d", 99876+doc*3+page), rgb(0x222222))
	c.footer("New", "Print", "Pageview")
	c.button("page-prev", w-93, h-22, 28, 18, "<", false)
	c.center(w-62, h-22, 27, 18, 9, fmt.Sprintf("%d/3", page+1), c.p.onDark)
	c.button("page-next", w-33, h-22, 28, 18, ">", false)
}

func (c *canvas) syntheticScan(x, y, w, h int, phase float64) {
	c.rect(x, y, w, h, rgb(0x161616))
	for yy := 2; yy < h-2; yy++ {
		for xx := 2; xx < w-2; xx++ {
			nx := (float64(xx) - float64(w)/2) / (float64(w) / 2)
			ny := (float64(yy) - float64(h)/2) / (float64(h) / 2)
			r := math.Sqrt(nx*nx + ny*ny)
			if r > .91 {
				continue
			}
			value := int(80 + 70*math.Sin(r*31+phase) + 38*math.Sin(nx*13)*math.Cos(ny*18))
			value = max(20, min(230, value))
			c.rect(x+xx, y+yy, 1, 1, color.RGBA{R: uint8(value), G: uint8(value), B: uint8(value), A: 255})
		}
	}
}
func (c *canvas) watermark(x, y, n int) {
	ink := rgb(0xe8e8e4)
	for i := 0; i < 3; i++ {
		c.circle(x, y, 18+i*14, ink, false)
	}
	c.line(x-43, y, x+43, y, ink)
	c.line(x, y-43, x, y+43, ink)
	c.center(x-37, y-13, 74, 24, 18, fmt.Sprintf("%02d", n+1), ink)
}

func (c *canvas) programs() {
	w, h := c.v.spec.width, c.v.spec.height
	c.rect(0, 0, w, h, c.p.navy)
	items := []struct {
		id    int
		label string
		icon  nativeui.Icon
	}{{1, "Calendar", nativeui.IconAdd}, {2, "Profile", nativeui.IconSettings}, {3, "Archive", nativeui.IconSearch}, {4, "Report", nativeui.IconPlay}, {5, "Notes", nativeui.IconCheck}, {7, "Comms", nativeui.IconForward}}
	for i, item := range items {
		x := 25 + i*70
		live := c.d.view(nativeapp.SurfaceID(item.id)).live
		control := c.control(fmt.Sprintf("open-%d", item.id), nativeui.KindButton, x, 5, 66, 58, item.label, nativeui.State{Selected: live}, 0)
		fill := c.p.blue
		if control.State.Hovered || control.State.Focused {
			fill = c.p.edge
		}
		c.bevel(x, 5, 66, 58, fill)
		c.text(x+4, 7, 58, 13, 9, item.label, c.p.onDark)
		if err := c.d.painters[10].DrawIcon(c.image, c.r(x+22, 24, 22, 22), item.icon, c.p.onDark); err != nil {
			c.err = err
		}
		status := "Open"
		if live {
			status = "Running"
		}
		c.center(x+3, 47, 60, 13, 9, status, c.p.onDark)
		c.rect(x+5, 66, 55, 2, c.p.edge)
	}
	caption := c.d.notice
	if caption == "" {
		caption = "MERRICK / PROGRAM INDEX"
	}
	c.text(30, 70, w-58, 13, 9, caption, c.p.onDark)
}

func (c *canvas) messages() {
	w, h := c.v.spec.width, c.v.spec.height
	c.rect(0, 0, w, h, c.p.navy)
	c.button("incoming", 26, 7, 134, 34, "Incoming", !c.d.state.Outgoing)
	c.button("outgoing", 26, 46, 134, 34, "Outgoing", c.d.state.Outgoing)
	for _, y := range []int{23, 62} {
		c.circle(144, y-3, 4, c.p.onDark, true)
		c.rect(138, y+3, 12, 6, c.p.onDark)
	}
	c.button("message-next", 167, 9, 54, 69, ">", false)
	labels := []string{"Review packet ready", "Archive update received", "Calendar changed", "Observer note received"}
	if c.d.state.Outgoing {
		labels = []string{"Summary dispatched", "Review acknowledged", "Record synchronized", "Archive request sent"}
	}
	c.text(28, 85, w-36, 15, 9, labels[c.d.state.Message], c.p.onDark)
	c.rect(25, h-17, w-31, 1, c.p.edge)
	c.text(30, h-15, 120, 13, 9, "COMMS / LOCAL", c.p.muted)
	c.text(w-40, h-17, 34, 16, 11, fmt.Sprintf("%02d", c.d.state.Message+1), c.p.onDark)
}
