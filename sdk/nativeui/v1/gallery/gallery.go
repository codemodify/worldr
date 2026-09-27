// Package gallery is a working reference interface composed of nativeui controls.
// Settings and the out-of-process Skin Studio example use the same renderer.
package gallery

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
)

type Values struct {
	Query                    string
	Running, Enabled, Locked bool
	Gain, Progress           float64
	Tab, Selection, Mode     int
}

type Gallery struct {
	Values        Values
	painter       *nativeui.Painter
	controller    nativeui.Controller
	controls      []nativeui.Control
	width, height int
}

func New(theme nativeui.Theme) (*Gallery, error) {
	p, err := nativeui.NewPainter(theme)
	if err != nil {
		return nil, err
	}
	g := &Gallery{painter: p, Values: Values{Enabled: true, Gain: 68, Progress: 42}}
	if err := g.controller.SetTheme(theme); err != nil {
		_ = p.Close()
		return nil, err
	}
	return g, nil
}

func (g *Gallery) SetTheme(theme nativeui.Theme) error {
	if err := g.painter.SetTheme(theme); err != nil {
		return err
	}
	return g.controller.SetTheme(theme)
}
func (g *Gallery) Semantics() nativeapp.SemanticTree { return g.controller.Semantics() }
func (g *Gallery) Close() error                      { return errors.Join(g.painter.Close(), g.controller.Close()) }

func (g *Gallery) Update(delta time.Duration) bool {
	if !g.Values.Running || !g.Values.Enabled {
		return false
	}
	g.Values.Progress += delta.Seconds() * 12
	if g.Values.Progress >= 100 {
		g.Values.Progress = 100
		g.Values.Running = false
	}
	return true
}

// Handle keeps application state separate from appearance. Text input accepts
// native text commits; physical Backspace edits the focused search field.
func (g *Gallery) Handle(event nativeapp.Event) (bool, error) {
	before := make([]nativeui.State, len(g.controls))
	for i, c := range g.controls {
		before[i] = g.controller.Decorate(c).State
	}
	a := g.controller.Handle(event)
	changed := a.Changed || a.ChangedFocus || a.Activated
	for i, c := range g.controls {
		if before[i] != g.controller.Decorate(c).State {
			changed = true
			break
		}
	}
	if a.Changed && a.ID == "gain" {
		g.Values.Gain = a.Value
		changed = true
	}
	if a.Activated {
		switch a.ID {
		case "run":
			if g.Values.Progress >= 100 {
				g.Values.Progress = 0
			}
			g.Values.Running = !g.Values.Running
		case "reset":
			g.Values.Progress = 0
			g.Values.Running = false
		case "enabled":
			g.Values.Enabled = !g.Values.Enabled
		case "locked":
			g.Values.Locked = !g.Values.Locked
		case "wideband":
			g.Values.Mode = 0
		case "deepscan":
			g.Values.Mode = 1
		case "overview":
			g.Values.Tab = 0
		case "details":
			g.Values.Tab = 1
		case "row-0":
			g.Values.Selection = 0
		case "row-1":
			g.Values.Selection = 1
		case "row-2":
			g.Values.Selection = 2
		}
		changed = true
	}
	if g.controller.FocusedID() == "query" {
		if event.Kind == nativeapp.TextCommit && utf8.ValidString(event.Text) && len(g.Values.Query)+len(event.Text) <= 256 {
			g.Values.Query += strings.ReplaceAll(strings.ReplaceAll(event.Text, "\n", ""), "\r", "")
			changed = true
		}
		if event.Kind == nativeapp.KeyInput && event.Pressed && (event.Key == "Backspace" || event.Keycode == 14) {
			r := []rune(g.Values.Query)
			if len(r) > 0 {
				g.Values.Query = string(r[:len(r)-1])
			}
			changed = true
		}
	}
	return changed, nil
}

func (g *Gallery) Render(width, height int) (*image.RGBA, error) {
	if width < 640 || height < 320 {
		return nil, fmt.Errorf("gallery needs at least 640x320 pixels")
	}
	g.width, g.height = width, height
	p := g.painter
	t := p.Theme()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), image.NewUniform(t.Palette.Background), image.Point{}, draw.Src)
	var failure error
	paint := func(err error) {
		if failure == nil {
			failure = err
		}
	}
	label := func(r image.Rectangle, text string, muted bool) {
		paint(p.DrawLabel(img, r, text, nativeui.LabelStyle{Muted: muted}))
	}
	margin, gap := 18, max(8, min(16, t.Metrics.Gap))
	controlH := max(26, min(40, t.Metrics.ControlHeight))
	if height < 400 {
		gap, controlH = 8, 26
	}
	paint(p.DrawToolbar(img, nativeui.Control{Bounds: image.Rect(10, 10, width-10, 58)}))
	label(image.Rect(24, 16, width/2, 39), "SKIN STUDIO  /  RESEARCH ARRAY", false)
	label(image.Rect(24, 37, width/2, 54), "One application. Shared controls. Live appearance.", true)
	runLabel, runIcon := "Run analysis", nativeui.IconPlay
	if g.Values.Running {
		runLabel, runIcon = "Pause", nativeui.IconPause
	}
	g.controls = []nativeui.Control{
		{ID: "run", Kind: nativeui.KindButton, Bounds: image.Rect(width-278, 18, width-110, 50), Label: runLabel, Icon: runIcon, State: nativeui.State{Disabled: !g.Values.Enabled}},
		{ID: "reset", Kind: nativeui.KindButton, Bounds: image.Rect(width-100, 18, width-22, 50), Label: "Reset"},
		{ID: "overview", Kind: nativeui.KindTab, Bounds: image.Rect(margin, 68, margin+130, 98), Label: "Overview", State: nativeui.State{Selected: g.Values.Tab == 0}},
		{ID: "details", Kind: nativeui.KindTab, Bounds: image.Rect(margin+138, 68, margin+268, 98), Label: "Diagnostics", State: nativeui.State{Selected: g.Values.Tab == 1}},
	}
	status := "READY"
	if g.Values.Running {
		status = "ACQUIRING"
	}
	if !g.Values.Enabled {
		status = "OFFLINE"
	}
	paint(p.DrawBadge(img, nativeui.Control{Bounds: image.Rect(width-174, 70, width-18, 96), Label: status, State: nativeui.State{Selected: g.Values.Running}}))
	top, bottom := 112, height-42
	leftRight := margin + (width-margin*2)*37/100
	paint(p.DrawPanel(img, nativeui.Control{Bounds: image.Rect(margin, top, leftRight, bottom)}))
	paint(p.DrawCard(img, nativeui.Control{Bounds: image.Rect(leftRight+gap, top, width-margin, bottom)}))
	x, y, r := margin+14, top+12, leftRight-14
	label(image.Rect(x, y, r, y+22), "Acquisition controls", false)
	y += 30
	g.controls = append(g.controls,
		nativeui.Control{ID: "enabled", Kind: nativeui.KindSwitch, Bounds: image.Rect(x, y, r, y+controlH), Label: "Array online", State: nativeui.State{Selected: g.Values.Enabled}},
		nativeui.Control{ID: "locked", Kind: nativeui.KindCheckbox, Bounds: image.Rect(x, y+controlH+gap, r, y+controlH*2+gap), Label: "Lock calibration", State: nativeui.State{Selected: g.Values.Locked}},
	)
	y += controlH*2 + gap*2
	g.controls = append(g.controls, nativeui.Control{ID: "gain", Kind: nativeui.KindSlider, Bounds: image.Rect(x, y, r, y+controlH+12), Label: fmt.Sprintf("Gain  %.0f%%", g.Values.Gain), Min: 0, Max: 100, Value: g.Values.Gain, Step: 1, State: nativeui.State{Disabled: g.Values.Locked}})
	y += controlH + gap + 12
	if y+controlH < bottom-8 {
		paint(p.DrawProgress(img, nativeui.Control{Bounds: image.Rect(x, y, r, y+controlH), Label: fmt.Sprintf("Analysis  %.0f%%", g.Values.Progress), Min: 0, Max: 100, Value: g.Values.Progress}))
	}
	if height >= 580 {
		y += controlH + 22
		paint(p.DrawSeparator(img, nativeui.Control{Bounds: image.Rect(x, y, r, y+1)}))
		y += 10
		label(image.Rect(x, y, r, y+24), "Capture profile", false)
		y += 30
		g.controls = append(g.controls,
			nativeui.Control{ID: "wideband", Kind: nativeui.KindRadio, Bounds: image.Rect(x, y, r, y+controlH), Label: "Wideband sweep", State: nativeui.State{Selected: g.Values.Mode == 0}},
			nativeui.Control{ID: "deepscan", Kind: nativeui.KindRadio, Bounds: image.Rect(x, y+controlH+gap, r, y+controlH*2+gap), Label: "Deep scan", State: nativeui.State{Selected: g.Values.Mode == 1}},
		)
		y += controlH*2 + gap + 12
		if y+24 < bottom {
			label(image.Rect(x, y, r, y+20), "24 sensors  /  48 kHz sampling", true)
		}
	}
	x, r = leftRight+gap+14, width-margin-14
	y = top + 12
	if g.Values.Tab == 0 {
		g.controls = append(g.controls, nativeui.Control{ID: "query", Kind: nativeui.KindField, Bounds: image.Rect(x, y, r, y+controlH), Label: "Filter channels", Text: g.Values.Query, Placeholder: "Filter channels…", Icon: nativeui.IconSearch})
		y += controlH + gap
		label(image.Rect(x, y, r, y+22), "CHANNEL                         SIGNAL / STATUS", true)
		y += 26
		for i, name := range []string{"Atmosphere", "Magnetosphere", "Deep field"} {
			if !strings.Contains(strings.ToLower(name), strings.ToLower(g.Values.Query)) {
				continue
			}
			if y+controlH > bottom-10 {
				break
			}
			g.controls = append(g.controls, nativeui.Control{ID: fmt.Sprintf("row-%d", i), Kind: nativeui.KindTableRow, Bounds: image.Rect(x, y, r, y+controlH), Label: fmt.Sprintf("%02d   %s", i+1, name), ValueText: []string{"98% / nominal", "84% / nominal", "62% / calibrating"}[i], State: nativeui.State{Selected: g.Values.Selection == i}})
			y += controlH + 4
		}
	} else {
		label(image.Rect(x, y, r, y+24), "Diagnostics / live values", false)
		y += 34
		for i, name := range []string{"Sensor coupling", "Signal clarity", "Thermal headroom"} {
			if y+controlH > bottom-10 {
				break
			}
			value := []float64{g.Values.Gain, 92, g.Values.Progress}[i]
			paint(p.DrawMeter(img, nativeui.Control{Bounds: image.Rect(x, y, r, y+controlH), Label: name, Min: 0, Max: 100, Value: value}))
			y += controlH + gap
		}
	}
	if err := g.controller.SetControlsWithin(g.controls, img.Bounds()); err != nil {
		return nil, err
	}
	for _, raw := range g.controls {
		c := g.controller.Decorate(raw)
		switch c.Kind {
		case nativeui.KindButton:
			paint(p.DrawButton(img, c))
		case nativeui.KindTab:
			paint(p.DrawTabs(img, []nativeui.Control{c}))
		case nativeui.KindField:
			paint(p.DrawField(img, c))
		case nativeui.KindSwitch:
			paint(p.DrawSwitch(img, c))
		case nativeui.KindCheckbox:
			paint(p.DrawCheckbox(img, c))
		case nativeui.KindRadio:
			paint(p.DrawRadio(img, c))
		case nativeui.KindSlider:
			paint(p.DrawSlider(img, c))
		case nativeui.KindTableRow:
			paint(p.DrawTableRow(img, c))
			// ValueText is semantic data; table applications define columns.
			label(image.Rect(c.Bounds.Min.X+c.Bounds.Dx()*54/100, c.Bounds.Min.Y, c.Bounds.Max.X-12, c.Bounds.Max.Y), c.ValueText, true)
		}
	}
	if height >= 500 {
		plot := image.Rect(leftRight+gap+14, top+236, width-margin-14, bottom-14)
		paint(p.DrawPanel(img, nativeui.Control{Bounds: plot}))
		label(image.Rect(plot.Min.X+12, plot.Min.Y+8, plot.Max.X-12, plot.Min.Y+30), fmt.Sprintf("CHANNEL %02d / SIGNAL ENVELOPE", g.Values.Selection+1), false)
		chart := image.Rect(plot.Min.X+12, plot.Min.Y+42, plot.Max.X-12, plot.Max.Y-32)
		if chart.Dy() > 12 {
			for i := 1; i < 5; i++ {
				y := chart.Min.Y + i*chart.Dy()/5
				draw.Draw(img, image.Rect(chart.Min.X, y, chart.Max.X, y+1), image.NewUniform(t.Palette.Border), image.Point{}, draw.Over)
			}
			for i := 1; i < 8; i++ {
				x := chart.Min.X + i*chart.Dx()/8
				draw.Draw(img, image.Rect(x, chart.Min.Y, x+1, chart.Max.Y), image.NewUniform(t.Palette.Border), image.Point{}, draw.Over)
			}
			frequency := float64(2 + g.Values.Selection*2 + g.Values.Mode*3)
			previous := chart.Min.Y + chart.Dy()/2
			for x := chart.Min.X; x < chart.Max.X; x++ {
				phase := float64(x-chart.Min.X)/float64(chart.Dx())*math.Pi*frequency*2 + g.Values.Progress*.05
				y := chart.Min.Y + chart.Dy()/2 + int((math.Sin(phase)*.75+math.Sin(phase*2.5)*.25)*float64(chart.Dy())*.38*g.Values.Gain/100)
				draw.Draw(img, image.Rect(x, min(previous, y), x+1, max(previous, y)+2), image.NewUniform(t.Palette.Accent), image.Point{}, draw.Over)
				previous = y
			}
		}
		label(image.Rect(plot.Min.X+12, plot.Max.Y-26, plot.Max.X-12, plot.Max.Y-6), "0.0 s             SIMULATED SIGNAL             2.0 s", true)
	}
	label(image.Rect(margin, height-32, width-margin, height-8), fmt.Sprintf("CHANNEL %02d  /  GAIN %.0f%%     •     Tab to focus  •  Space to activate  •  Arrows to adjust", g.Values.Selection+1, g.Values.Gain), true)
	return img, failure
}
