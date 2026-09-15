package icontheme

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

const maxSVGBytes = 256 << 10
const maxSVGEdge = 256

// RasterSVG reads a simple icon SVG (rect/circle/ellipse/polygon/path)
// into BGRA at want×want (default DefaultWant). Complex filters/text fail.
func RasterSVG(path string, want int) (pix []byte, w, h, stride int, err error) {
	if want < 1 {
		want = DefaultWant
	}
	if want > maxSVGEdge {
		want = maxSVGEdge
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxSVGBytes+1))
	if err != nil {
		return nil, 0, 0, 0, err
	}
	if len(raw) > maxSVGBytes {
		return nil, 0, 0, 0, fmt.Errorf("svg too large")
	}
	return rasterSVGBytes(raw, want)
}

func rasterSVGBytes(raw []byte, want int) ([]byte, int, int, int, error) {
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity

	var vb [4]float64
	haveVB := false
	type frame struct{ fill color4 }
	stack := []frame{{fill: color4{0, 0, 0, 255}}}
	var draws []drawCmd

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, 0, 0, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			attrs := attrMap(t.Attr)
			cur := stack[len(stack)-1]
			if f, ok := parseFill(attrs["fill"]); ok {
				cur.fill = f
			}
			if name == "svg" {
				if v, ok := parseViewBox(attrs["viewbox"]); ok {
					vb, haveVB = v, true
				} else if ww, okw := parseFloat(attrs["width"]); okw {
					hh, _ := parseFloat(attrs["height"])
					if hh <= 0 {
						hh = ww
					}
					vb, haveVB = [4]float64{0, 0, ww, hh}, true
				}
				stack = append(stack, cur)
				continue
			}
			if name == "g" {
				stack = append(stack, cur)
				continue
			}
			pts, ok := shapePoints(name, attrs)
			if ok && len(pts) >= 3 {
				draws = append(draws, drawCmd{pts: pts, fill: cur.fill})
			}
			if name != "path" && name != "polygon" && name != "polyline" && name != "rect" &&
				name != "circle" && name != "ellipse" {
				stack = append(stack, cur)
			}
		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "g" {
				if len(stack) > 1 {
					stack = stack[:len(stack)-1]
				}
			}
		}
	}
	if !haveVB || vb[2] <= 0 || vb[3] <= 0 {
		vb = [4]float64{0, 0, float64(want), float64(want)}
	}
	out := make([]byte, want*want*4)
	sx := float64(want) / vb[2]
	sy := float64(want) / vb[3]
	for _, d := range draws {
		dst := make([]pt, len(d.pts))
		for i, p := range d.pts {
			dst[i] = pt{(p.x - vb[0]) * sx, (p.y - vb[1]) * sy}
		}
		fillPoly(out, want, want, want*4, dst, d.fill)
	}
	if len(draws) == 0 {
		return nil, 0, 0, 0, fmt.Errorf("svg: no drawable shapes")
	}
	return out, want, want, want * 4, nil
}

type pt struct{ x, y float64 }

type color4 struct{ r, g, b, a byte }

type drawCmd struct {
	pts  []pt
	fill color4
}

func attrMap(attrs []xml.Attr) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[strings.ToLower(a.Name.Local)] = strings.TrimSpace(a.Value)
	}
	return m
}

func parseViewBox(s string) ([4]float64, bool) {
	var z [4]float64
	s = strings.ReplaceAll(s, ",", " ")
	fs := strings.Fields(s)
	if len(fs) < 4 {
		return z, false
	}
	for i := 0; i < 4; i++ {
		v, ok := parseFloat(fs[i])
		if !ok {
			return z, false
		}
		z[i] = v
	}
	return z, z[2] > 0 && z[3] > 0
}

func parseFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "px")
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

func parseFill(s string) (color4, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "none" || s == "transparent" {
		return color4{}, false
	}
	switch s {
	case "black", "currentcolor":
		return color4{0, 0, 0, 255}, true
	case "white":
		return color4{255, 255, 255, 255}, true
	case "red":
		return color4{255, 0, 0, 255}, true
	}
	if strings.HasPrefix(s, "#") {
		hex := s[1:]
		if len(hex) == 3 {
			return color4{unhex2(hex[0]), unhex2(hex[1]), unhex2(hex[2]), 255}, true
		}
		if len(hex) == 6 {
			return color4{unhex(hex[0:2]), unhex(hex[2:4]), unhex(hex[4:6]), 255}, true
		}
	}
	if strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(s, "rgb("), ")")
		inner = strings.ReplaceAll(inner, ",", " ")
		fs := strings.Fields(inner)
		if len(fs) >= 3 {
			r, _ := strconv.Atoi(fs[0])
			g, _ := strconv.Atoi(fs[1])
			b, _ := strconv.Atoi(fs[2])
			return color4{clampB(r), clampB(g), clampB(b), 255}, true
		}
	}
	return color4{}, false
}

func unhex2(c byte) byte {
	v := unhexNibble(c)
	return v<<4 | v
}

func unhex(s string) byte {
	if len(s) < 2 {
		return 0
	}
	return unhexNibble(s[0])<<4 | unhexNibble(s[1])
}

func unhexNibble(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

func clampB(n int) byte {
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return byte(n)
}

func shapePoints(name string, a map[string]string) ([]pt, bool) {
	switch name {
	case "rect":
		x, _ := parseFloat(a["x"])
		y, _ := parseFloat(a["y"])
		w, okw := parseFloat(a["width"])
		h, okh := parseFloat(a["height"])
		if !okw || !okh || w <= 0 || h <= 0 {
			return nil, false
		}
		return []pt{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}, true
	case "circle":
		cx, _ := parseFloat(a["cx"])
		cy, _ := parseFloat(a["cy"])
		r, ok := parseFloat(a["r"])
		if !ok || r <= 0 {
			return nil, false
		}
		return oval(cx, cy, r, r, 24), true
	case "ellipse":
		cx, _ := parseFloat(a["cx"])
		cy, _ := parseFloat(a["cy"])
		rx, okx := parseFloat(a["rx"])
		ry, oky := parseFloat(a["ry"])
		if !okx || !oky || rx <= 0 || ry <= 0 {
			return nil, false
		}
		return oval(cx, cy, rx, ry, 24), true
	case "polygon", "polyline":
		return parsePoints(a["points"])
	case "path":
		return parsePath(a["d"])
	}
	return nil, false
}

func oval(cx, cy, rx, ry float64, n int) []pt {
	out := make([]pt, n)
	for i := 0; i < n; i++ {
		th := 2 * math.Pi * float64(i) / float64(n)
		out[i] = pt{cx + rx*math.Cos(th), cy + ry*math.Sin(th)}
	}
	return out
}

func parsePoints(s string) ([]pt, bool) {
	s = strings.ReplaceAll(s, ",", " ")
	fs := strings.Fields(s)
	if len(fs) < 6 || len(fs)%2 != 0 {
		return nil, false
	}
	out := make([]pt, 0, len(fs)/2)
	for i := 0; i+1 < len(fs); i += 2 {
		x, okx := parseFloat(fs[i])
		y, oky := parseFloat(fs[i+1])
		if !okx || !oky {
			return nil, false
		}
		out = append(out, pt{x, y})
	}
	return out, len(out) >= 3
}

func parsePath(d string) ([]pt, bool) {
	toks := splitPath(d)
	if len(toks) == 0 {
		return nil, false
	}
	var out []pt
	var cx, cy, sx, sy float64
	i := 0
	cmd := byte(0)
	for i < len(toks) {
		if len(toks[i]) == 1 && isCmd(toks[i][0]) {
			cmd = toks[i][0]
			i++
		}
		if cmd == 0 {
			break
		}
		rel := cmd >= 'a'
		c := cmd
		if rel {
			c -= 'a' - 'A'
		}
		take := func() (float64, bool) {
			if i >= len(toks) {
				return 0, false
			}
			v, ok := parseFloat(toks[i])
			i++
			return v, ok
		}
		switch c {
		case 'M', 'L':
			x, okx := take()
			y, oky := take()
			if !okx || !oky {
				return nil, false
			}
			if rel {
				x += cx
				y += cy
			}
			cx, cy = x, y
			if c == 'M' {
				sx, sy = cx, cy
			}
			out = append(out, pt{cx, cy})
			if c == 'M' {
				cmd = 'L'
				if rel {
					cmd = 'l'
				}
			}
		case 'H':
			x, ok := take()
			if !ok {
				return nil, false
			}
			if rel {
				x += cx
			}
			cx = x
			out = append(out, pt{cx, cy})
		case 'V':
			y, ok := take()
			if !ok {
				return nil, false
			}
			if rel {
				y += cy
			}
			cy = y
			out = append(out, pt{cx, cy})
		case 'Z':
			cx, cy = sx, sy
			out = append(out, pt{cx, cy})
		case 'C':
			x1, ok1 := take()
			y1, ok2 := take()
			x2, ok3 := take()
			y2, ok4 := take()
			x, ok5 := take()
			y, ok6 := take()
			if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
				return nil, false
			}
			if rel {
				x1 += cx
				y1 += cy
				x2 += cx
				y2 += cy
				x += cx
				y += cy
			}
			out = append(out, cubic(cx, cy, x1, y1, x2, y2, x, y, 6)...)
			cx, cy = x, y
		case 'Q':
			x1, ok1 := take()
			y1, ok2 := take()
			x, ok3 := take()
			y, ok4 := take()
			if !ok1 || !ok2 || !ok3 || !ok4 {
				return nil, false
			}
			if rel {
				x1 += cx
				y1 += cy
				x += cx
				y += cy
			}
			out = append(out, quad(cx, cy, x1, y1, x, y, 5)...)
			cx, cy = x, y
		default:
			return nil, false
		}
	}
	return out, len(out) >= 3
}

func isCmd(c byte) bool {
	return (c >= 'A' && c <= 'Z' && c != 'E') || (c >= 'a' && c <= 'z' && c != 'e')
}

func splitPath(d string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for i := 0; i < len(d); i++ {
		c := d[i]
		if c == ',' || c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			flush()
			continue
		}
		if isCmd(c) {
			flush()
			out = append(out, string(c))
			continue
		}
		if c == '-' && b.Len() > 0 {
			flush()
			b.WriteByte(c)
			continue
		}
		b.WriteByte(c)
	}
	flush()
	return out
}

func cubic(x0, y0, x1, y1, x2, y2, x3, y3 float64, n int) []pt {
	out := make([]pt, 0, n)
	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		x := u*u*u*x0 + 3*u*u*t*x1 + 3*u*t*t*x2 + t*t*t*x3
		y := u*u*u*y0 + 3*u*u*t*y1 + 3*u*t*t*y2 + t*t*t*y3
		out = append(out, pt{x, y})
	}
	return out
}

func quad(x0, y0, x1, y1, x2, y2 float64, n int) []pt {
	out := make([]pt, 0, n)
	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		x := u*u*x0 + 2*u*t*x1 + t*t*x2
		y := u*u*y0 + 2*u*t*y1 + t*t*y2
		out = append(out, pt{x, y})
	}
	return out
}

func fillPoly(dst []byte, w, h, stride int, pts []pt, c color4) {
	if len(pts) < 3 || c.a == 0 {
		return
	}
	minY, maxY := pts[0].y, pts[0].y
	for _, p := range pts {
		if p.y < minY {
			minY = p.y
		}
		if p.y > maxY {
			maxY = p.y
		}
	}
	y0 := int(math.Floor(minY))
	y1 := int(math.Ceil(maxY))
	if y0 < 0 {
		y0 = 0
	}
	if y1 > h {
		y1 = h
	}
	n := len(pts)
	xs := make([]float64, 0, n)
	for y := y0; y < y1; y++ {
		scan := float64(y) + 0.5
		xs = xs[:0]
		j := n - 1
		for i := 0; i < n; i++ {
			yi, yj := pts[i].y, pts[j].y
			if (yi <= scan && yj > scan) || (yj <= scan && yi > scan) {
				t := (scan - yi) / (yj - yi)
				xs = append(xs, pts[i].x+t*(pts[j].x-pts[i].x))
			}
			j = i
		}
		// insertion sort
		for i := 1; i < len(xs); i++ {
			v := xs[i]
			k := i
			for k > 0 && xs[k-1] > v {
				xs[k] = xs[k-1]
				k--
			}
			xs[k] = v
		}
		for i := 0; i+1 < len(xs); i += 2 {
			x0 := int(math.Floor(xs[i]))
			x1 := int(math.Ceil(xs[i+1]))
			if x0 < 0 {
				x0 = 0
			}
			if x1 > w {
				x1 = w
			}
			row := y * stride
			for x := x0; x < x1; x++ {
				di := row + x*4
				if di+3 >= len(dst) {
					continue
				}
				// BGRA
				dst[di+0] = c.b
				dst[di+1] = c.g
				dst[di+2] = c.r
				dst[di+3] = c.a
			}
		}
	}
}
