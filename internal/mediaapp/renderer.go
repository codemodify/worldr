package mediaapp

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"unicode"

	"github.com/codemodify/worldr/internal/render"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

const minWidth, minHeight, maxWidth, maxHeight = 640, 360, 1920, 1080

type playerView struct {
	Title, Message                        string
	Loaded, Paused, Ended, Muted, Focused bool
	Position, Duration, Volume            float64
	VideoWidth, VideoHeight               int
}

type playerLayout struct {
	Video, Seek, Play, Stop, Back, Forward, Mute, Volume image.Rectangle
}

type playerRenderer struct {
	image       *image.RGBA
	chrome      *image.RGBA
	texture     *render.Texture
	face, small font.Face
	font        *opentype.Font
	scale       float64
}

func newPlayerRenderer(width, height int) (*playerRenderer, error) {
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}
	r := &playerRenderer{font: parsed}
	err = r.resize(width, height)
	if err != nil {
		r.close()
		return nil, err
	}
	return r, nil
}

func (r *playerRenderer) close() {
	if r.face != nil {
		_ = r.face.Close()
	}
	if r.small != nil {
		_ = r.small.Close()
	}
}

func (r *playerRenderer) resize(width, height int) error {
	if width < minWidth || height < minHeight || width > maxWidth || height > maxHeight {
		return fmt.Errorf("media player size is out of bounds")
	}
	// Fit the chrome to both axes so a short, wide window keeps usable controls.
	scale := max(1, min(float64(width)/960, float64(height)/600))
	if r.face == nil || scale != r.scale {
		face, err := opentype.NewFace(r.font, &opentype.FaceOptions{Size: 16 * scale, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return err
		}
		small, err := opentype.NewFace(r.font, &opentype.FaceOptions{Size: 12 * scale, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			_ = face.Close()
			return err
		}
		r.close()
		r.face, r.small, r.scale = face, small, scale
	}
	r.image = image.NewRGBA(image.Rect(0, 0, width, height))
	r.chrome = nil
	if r.texture == nil {
		var err error
		r.texture, err = render.NewTexture(width, height, r.image.Pix)
		return err
	}
	return r.texture.Replace(width, height, r.image.Pix)
}

func (r *playerRenderer) layout() playerLayout {
	l := r.logicalLayout()
	for _, rect := range []*image.Rectangle{&l.Video, &l.Seek, &l.Play, &l.Stop, &l.Back, &l.Forward, &l.Mute, &l.Volume} {
		*rect = r.scaledRect(*rect)
	}
	return l
}

func (r *playerRenderer) logicalLayout() playerLayout {
	w, h := r.extent()
	left, right := 58, w-58
	deck := (right - left) * 3 / 5
	back := left + deck*22/100
	play := left + deck*60/100
	forward := left + deck*83/100
	volume := left + deck + 24
	return playerLayout{
		Video:   image.Rect(40, 80, w-40, h-146),
		Seek:    image.Rect(left, h-118, right, h-94),
		Back:    image.Rect(left, h-83, back, h-40),
		Play:    image.Rect(back+3, h-88, play, h-29),
		Forward: image.Rect(play+3, h-83, forward, h-40),
		Stop:    image.Rect(forward+3, h-83, left+deck, h-40),
		Mute:    image.Rect(volume, h-58, volume+32, h-30),
		Volume:  image.Rect(volume+10, h-85, right-10, h-61),
	}
}

func (r *playerRenderer) extent() (int, int) {
	return int(float64(r.image.Rect.Dx()) / r.scale), int(float64(r.image.Rect.Dy()) / r.scale)
}
func (r *playerRenderer) scaledPoint(p image.Point) image.Point {
	return image.Pt(int(math.Round(float64(p.X)*r.scale)), int(math.Round(float64(p.Y)*r.scale)))
}
func (r *playerRenderer) scaledRect(rect image.Rectangle) image.Rectangle {
	return image.Rectangle{Min: r.scaledPoint(rect.Min), Max: r.scaledPoint(rect.Max)}
}

func mediaColor(c uint32) color.RGBA { return color.RGBA{byte(c >> 16), byte(c >> 8), byte(c), 255} }
func (r *playerRenderer) fill(rect image.Rectangle, c uint32) {
	draw.Draw(r.image, r.scaledRect(rect), image.NewUniform(mediaColor(c)), image.Point{}, draw.Src)
}
func (r *playerRenderer) polygon(c uint32, points ...image.Point) {
	path := make([][2]float64, len(points))
	for i, p := range points {
		path[i] = [2]float64{float64(p.X), float64(p.Y)}
	}
	r.path(c, path...)
}

func cleanTitle(s string) string {
	var out strings.Builder
	for _, c := range s {
		if !unicode.IsControl(c) && !unicode.Is(unicode.Cf, c) {
			out.WriteRune(c)
		}
	}
	return out.String()
}
func (r *playerRenderer) label(rect image.Rectangle, text string, c uint32, small bool) {
	rect = r.scaledRect(rect).Intersect(r.image.Rect)
	if rect.Empty() {
		return
	}
	face := r.face
	if small {
		face = r.small
	}
	chars := []rune(cleanTitle(text))
	limit := max(1, rect.Dx()/6+1)
	if len(chars) > limit {
		chars = chars[:limit]
	}
	for len(chars) > 0 && font.MeasureString(face, string(chars)).Ceil() > rect.Dx() {
		chars = chars[:len(chars)-1]
	}
	d := font.Drawer{Dst: r.image.SubImage(rect).(*image.RGBA), Src: image.NewUniform(mediaColor(c)), Face: face, Dot: fixed.P(rect.Min.X, rect.Min.Y+face.Metrics().Ascent.Ceil())}
	d.DrawString(string(chars))
}
func mediaTime(seconds float64) string {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		seconds = 0
	}
	n := int64(min(seconds, 359999))
	if n >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", n/3600, n/60%60, n%60)
	}
	return fmt.Sprintf("%02d:%02d", n/60, n%60)
}

// path rasterizes fractional logical coordinates for fine rails and circular
// controls at any supported display scale.
func (r *playerRenderer) path(c uint32, points ...[2]float64) {
	if len(points) < 3 {
		return
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i := range points {
		points[i][0] *= r.scale
		points[i][1] *= r.scale
		minX, minY = min(minX, points[i][0]), min(minY, points[i][1])
		maxX, maxY = max(maxX, points[i][0]), max(maxY, points[i][1])
	}
	bounds := image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX))+1, int(math.Ceil(maxY))+1).Intersect(r.image.Rect)
	if bounds.Empty() {
		return
	}
	z := vector.NewRasterizer(bounds.Dx(), bounds.Dy())
	z.MoveTo(float32(points[0][0]-float64(bounds.Min.X)), float32(points[0][1]-float64(bounds.Min.Y)))
	for _, p := range points[1:] {
		z.LineTo(float32(p[0]-float64(bounds.Min.X)), float32(p[1]-float64(bounds.Min.Y)))
	}
	z.ClosePath()
	z.Draw(r.image, bounds, image.NewUniform(mediaColor(c)), image.Point{})
}

func (r *playerRenderer) line(c uint32, width float64, points ...image.Point) {
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		dx, dy := float64(b.X-a.X), float64(b.Y-a.Y)
		length := math.Hypot(dx, dy)
		if length == 0 {
			continue
		}
		x, y := -dy*width/(2*length), dx*width/(2*length)
		r.path(c, [2]float64{float64(a.X) + x, float64(a.Y) + y}, [2]float64{float64(b.X) + x, float64(b.Y) + y}, [2]float64{float64(b.X) - x, float64(b.Y) - y}, [2]float64{float64(a.X) - x, float64(a.Y) - y})
	}
}

func (r *playerRenderer) arc(c uint32, center image.Point, radius, width, start, end float64) {
	segments := max(2, int(math.Ceil((end-start)*radius/3)))
	points := make([][2]float64, 0, (segments+1)*2)
	for i := 0; i <= segments; i++ {
		a := start + (end-start)*float64(i)/float64(segments)
		points = append(points, [2]float64{float64(center.X) + math.Cos(a)*radius, float64(center.Y) + math.Sin(a)*radius})
	}
	for i := segments; i >= 0; i-- {
		a := start + (end-start)*float64(i)/float64(segments)
		points = append(points, [2]float64{float64(center.X) + math.Cos(a)*(radius-width), float64(center.Y) + math.Sin(a)*(radius-width)})
	}
	r.path(c, points...)
}

func (r *playerRenderer) beveled(rect image.Rectangle, bevel int, c uint32) {
	x, y, u, v := rect.Min.X, rect.Min.Y, rect.Max.X, rect.Max.Y
	r.polygon(c, image.Pt(x+bevel, y), image.Pt(u-bevel, y), image.Pt(u, y+bevel), image.Pt(u, v-bevel), image.Pt(u-bevel, v), image.Pt(x+bevel, v), image.Pt(x, v-bevel), image.Pt(x, y+bevel))
}

func (r *playerRenderer) hexagon(p image.Point, radius int, c uint32) {
	d := radius * 7 / 8
	r.polygon(c, image.Pt(p.X-radius, p.Y), image.Pt(p.X-radius/2, p.Y-d), image.Pt(p.X+radius/2, p.Y-d), image.Pt(p.X+radius, p.Y), image.Pt(p.X+radius/2, p.Y+d), image.Pt(p.X-radius/2, p.Y+d))
}

func (r *playerRenderer) paintChrome() {
	w, h := r.extent()
	l := r.logicalLayout()
	const navy, teal, cyan = uint32(0x040c19), uint32(0x047e96), uint32(0x87f4f1)
	r.fill(image.Rect(0, 0, w+1, h+1), navy)
	// This background and the mechanical frame are cached between video frames.
	// The hexagonal grid never overlays the decoded picture.
	for y := -12; y < h+20; y += 28 {
		for x := -24; x < w+24; x += 48 {
			offset := 0
			if (y+12)/28%2 != 0 {
				offset = 24
			}
			cx := x + offset
			r.line(0x081723, .6, image.Pt(cx-16, y), image.Pt(cx-8, y-14), image.Pt(cx+8, y-14), image.Pt(cx+16, y), image.Pt(cx+8, y+14), image.Pt(cx-8, y+14), image.Pt(cx-16, y))
		}
	}
	// Broken circuits and twin offset rails extend beyond the main chassis.
	r.line(0x12374a, 3, image.Pt(0, h/2-18), image.Pt(13, h/2-18), image.Pt(28, h/2-33), image.Pt(28, 72), image.Pt(56, 44), image.Pt(w/3, 44))
	r.line(0x12374a, 2, image.Pt(w, h/2+18), image.Pt(w-11, h/2+18), image.Pt(w-11, h-103), image.Pt(w-43, h-71))
	r.line(0x14768a, 1.2, image.Pt(22, 96), image.Pt(22, 79), image.Pt(51, 50), image.Pt(w/3-12, 50), image.Pt(w/3-2, 60), image.Pt(w/2-59, 60))
	r.line(0x319aae, 1, image.Pt(17, 110), image.Pt(17, h-126), image.Pt(48, h-95), image.Pt(48, h-31), image.Pt(65, h-23), image.Pt(w-123, h-23), image.Pt(w-95, h-42), image.Pt(w-49, h-42), image.Pt(w-19, h-72), image.Pt(w-19, 84), image.Pt(w-52, 51))
	r.line(0x155267, 1, image.Pt(12, 166), image.Pt(12, h-128), image.Pt(43, h-97), image.Pt(43, h-29), image.Pt(61, h-26), image.Pt(w/2, h-26))

	// Raised header with swept rails, rather than a conventional title bar.
	x := w / 2
	r.line(0x258595, 1.2, image.Pt(32, 33), image.Pt(x-140, 33), image.Pt(x-128, 21))
	r.line(0x258595, 1.2, image.Pt(x+128, 21), image.Pt(x+140, 33), image.Pt(w-32, 33))
	r.line(cyan, 1.5, image.Pt(26, 27), image.Pt(37, 37), image.Pt(x-144, 37), image.Pt(x-138, 31))
	r.line(cyan, 1.5, image.Pt(x+138, 31), image.Pt(x+144, 37), image.Pt(w-37, 37), image.Pt(w-26, 27))
	r.polygon(0x123749, image.Pt(x-131, 22), image.Pt(x-112, 9), image.Pt(x-82, 9), image.Pt(x-76, 14), image.Pt(x+76, 14), image.Pt(x+82, 9), image.Pt(x+112, 9), image.Pt(x+131, 22), image.Pt(x+118, 41), image.Pt(x-118, 41))
	r.line(cyan, 1.5, image.Pt(x-139, 21), image.Pt(x-127, 21), image.Pt(x-113, 7), image.Pt(x-98, 7))
	r.line(cyan, 1.5, image.Pt(x+98, 7), image.Pt(x+113, 7), image.Pt(x+127, 21), image.Pt(x+139, 21))
	r.label(image.Rect(x-91, 19, x+110, 39), "W O R L D R  /  A V", 0xb8fff9, false)
	for _, side := range []int{64, w - 101} {
		for i := 0; i < 3; i++ {
			t := side + i*13
			r.polygon(0x53cad3, image.Pt(t, 34), image.Pt(t+6, 28), image.Pt(t+16, 28), image.Pt(t+10, 34))
		}
	}

	// Broad asymmetric teal shell, stepped side armor, and a fine luminous lip.
	r.polygon(teal, image.Pt(26, 83), image.Pt(51, 58), image.Pt(x-93, 58), image.Pt(x-79, 68), image.Pt(x+102, 68), image.Pt(x+118, 53), image.Pt(w-61, 53), image.Pt(w-27, 86), image.Pt(w-27, h-223), image.Pt(w-14, h-210), image.Pt(w-14, h-137), image.Pt(w-49, h-102), image.Pt(w-136, h-102), image.Pt(w-153, h-88), image.Pt(86, h-88), image.Pt(27, h-147), image.Pt(27, h-186), image.Pt(20, h-194), image.Pt(20, h/2-8), image.Pt(26, h/2-15))
	// Slightly darker outer shoulders give the frame depth without bloom over video.
	r.polygon(0x056778, image.Pt(w-26, 98), image.Pt(w-26, h-223), image.Pt(w-14, h-210), image.Pt(w-14, h-137), image.Pt(w-26, h-149))
	r.beveled(image.Rect(33, 73, w-33, h-139), 14, 0x9afaf3)
	r.beveled(image.Rect(35, 75, w-35, h-141), 12, 0x123d4f)
	r.line(0x57d7df, 1, image.Pt(40, 87), image.Pt(47, 80), image.Pt(w-41, 80), image.Pt(w-41, h-153), image.Pt(w-48, h-146), image.Pt(40, h-146), image.Pt(40, 87))
	r.fill(l.Video, 0x020911)
	for i := 0; i < 3; i++ {
		t := 61 + i*58
		r.polygon(uint32(0x3cc7d1)-uint32(i)*0x0a1816, image.Pt(t, 67), image.Pt(t+6, 61), image.Pt(t+55, 61), image.Pt(t+49, 67))
	}
	for i := 0; i < 4; i++ {
		t := w - 233 + i*40
		r.line(0x39bbca, 1, image.Pt(t, 62), image.Pt(t+31, 62), image.Pt(t+35, 66), image.Pt(t+4, 66), image.Pt(t, 62))
	}
	for i := 0; i < 6; i++ {
		y := h - 208 + i*11
		r.arc(0x073c50, image.Pt(w-21, y), 2.8, 1, 0, 2*math.Pi)
	}
	for y := 123; y < h-166; y += 7 {
		r.line(0x3baabe, 1, image.Pt(29, y), image.Pt(32, y+3))
	}
	r.line(0x35bdd0, 1, image.Pt(w-23, 85), image.Pt(w-23, h-226), image.Pt(w-10, h-213), image.Pt(w-10, h-153))

	// Independent angular transport keys. The dominant play key projects below
	// its neighbors; the separate volume island follows the same chassis cut.
	for _, b := range []image.Rectangle{l.Back, l.Forward, l.Stop} {
		r.beveled(b.Inset(-2), 12, 0x042334)
		r.beveled(b, 10, teal)
		r.line(0x2bb5c5, 1, image.Pt(b.Min.X+11, b.Min.Y+1), image.Pt(b.Max.X-11, b.Min.Y+1))
		r.line(0x28a4b6, 1, image.Pt(b.Min.X+10, b.Max.Y-3), image.Pt(b.Max.X-10, b.Max.Y-3))
	}
	r.beveled(l.Play.Inset(-2), 16, 0x042334)
	r.beveled(l.Play, 14, 0x0aa4b6)
	r.line(0x8ffaf3, 1.2, image.Pt(l.Play.Min.X+15, l.Play.Min.Y+1), image.Pt(l.Play.Max.X-15, l.Play.Min.Y+1))
	r.line(0x176073, 1, image.Pt(l.Play.Min.X+16, l.Play.Max.Y+5), image.Pt(l.Play.Max.X-16, l.Play.Max.Y+5))
	v := image.Rect(l.Mute.Min.X-5, h-88, w-58, h-28)
	r.beveled(v, 12, 0x092c40)
	r.beveled(v.Inset(2), 10, 0x086c83)
	r.line(0x2dbbc8, 1, image.Pt(v.Min.X+13, v.Min.Y+3), image.Pt(v.Max.X-13, v.Min.Y+3))
	r.line(0x06374b, 1, image.Pt(v.Min.X+5, h-59), image.Pt(v.Max.X-5, h-59))
	// Two small reference marks finish the open lower rail.
	for _, t := range []int{31, w - 57} {
		r.line(0x246778, 1, image.Pt(t, h-17), image.Pt(t+25, h-17))
		r.arc(0x51c1ce, image.Pt(t, h-17), 2.3, .8, 0, 2*math.Pi)
	}
}

func (r *playerRenderer) paint(s playerView, video *image.RGBA) error {
	w, h := r.extent()
	l := r.logicalLayout()
	const cyan, pale, ink = uint32(0x75f5ef), uint32(0xb9fff8), uint32(0x043647)
	if r.chrome == nil {
		r.paintChrome()
		r.chrome = image.NewRGBA(r.image.Rect)
		copy(r.chrome.Pix, r.image.Pix)
	} else {
		copy(r.image.Pix, r.chrome.Pix)
	}
	if video != nil {
		draw.Draw(r.image, r.layout().Video, video, video.Rect.Min, draw.Src)
	}
	status := "LOADING"
	if s.Loaded {
		status = "PLAYING"
	}
	if s.Paused {
		status = "PAUSED"
	}
	if s.Ended {
		status = "REPLAY"
	}
	if s.Message != "" {
		status = "UNAVAILABLE"
	}
	r.label(image.Rect(58, h-138, w-205, h-120), s.Title, pale, true)
	r.hexagon(image.Pt(w-164, h-131), 3, cyan)
	r.label(image.Rect(w-153, h-138, w-50, h-120), status, pale, true)

	// The full track remains a generous hit target; only the inner rail and
	// outlined hexagonal thumb are visible.
	seekY := (l.Seek.Min.Y + l.Seek.Max.Y) / 2
	r.fill(image.Rect(l.Seek.Min.X, seekY-3, l.Seek.Max.X, seekY+3), 0x033548)
	progress := float64(0)
	if s.Duration > 0 {
		progress = max(0, min(1, s.Position/s.Duration))
	}
	end := l.Seek.Min.X + int(float64(l.Seek.Dx())*progress)
	r.fill(image.Rect(l.Seek.Min.X, seekY-1, end, seekY+1), pale)
	if s.Loaded {
		r.hexagon(image.Pt(end, seekY), 7, ink)
		r.hexagon(image.Pt(end, seekY), 5, pale)
		r.hexagon(image.Pt(end, seekY), 2, 0x04859d)
	}

	icon := pale
	if !s.Loaded {
		icon = 0x418e9b
	}
	center := func(b image.Rectangle) image.Point { return image.Pt((b.Min.X+b.Max.X)/2, (b.Min.Y+b.Max.Y)/2) }
	p := center(l.Play)
	if s.Paused || s.Ended || !s.Loaded {
		r.polygon(ink, image.Pt(p.X-6, p.Y-12), image.Pt(p.X+11, p.Y), image.Pt(p.X-6, p.Y+12))
	} else {
		r.beveled(image.Rect(p.X-9, p.Y-12, p.X-3, p.Y+12), 1, ink)
		r.beveled(image.Rect(p.X+3, p.Y-12, p.X+9, p.Y+12), 1, ink)
	}
	p = center(l.Stop)
	r.fill(image.Rect(p.X-6, p.Y-6, p.X+6, p.Y+6), icon)
	for _, b := range []image.Rectangle{l.Back, l.Forward} {
		p = center(b)
		dir := 1
		if b == l.Back {
			dir = -1
		}
		for _, dx := range []int{-5, 5} {
			r.polygon(icon, image.Pt(p.X+dx-dir*5, p.Y-8), image.Pt(p.X+dx+dir*5, p.Y), image.Pt(p.X+dx-dir*5, p.Y+8))
		}
	}
	p = center(l.Mute)
	r.fill(image.Rect(p.X-8, p.Y-3, p.X-3, p.Y+3), icon)
	r.polygon(icon, image.Pt(p.X-3, p.Y-3), image.Pt(p.X+2, p.Y-7), image.Pt(p.X+2, p.Y+7), image.Pt(p.X-3, p.Y+3))
	if s.Muted {
		r.line(icon, 1.5, image.Pt(p.X+5, p.Y-4), image.Pt(p.X+10, p.Y+4))
		r.line(icon, 1.5, image.Pt(p.X+5, p.Y+4), image.Pt(p.X+10, p.Y-4))
	} else {
		r.arc(icon, image.Pt(p.X, p.Y), 8, 1.2, -.7, .7)
		r.arc(icon, image.Pt(p.X, p.Y), 11, 1.2, -.7, .7)
	}
	p = center(l.Volume)
	r.fill(image.Rect(l.Volume.Min.X, p.Y-2, l.Volume.Max.X, p.Y+2), ink)
	x := l.Volume.Min.X + int(max(0, min(100, s.Volume))*float64(l.Volume.Dx())/100)
	r.fill(image.Rect(l.Volume.Min.X, p.Y-1, x, p.Y+1), cyan)
	r.hexagon(image.Pt(x, p.Y), 5, ink)
	r.hexagon(image.Pt(x, p.Y), 3, pale)
	r.label(image.Rect(l.Mute.Max.X+6, h-51, w-67, h-31), mediaTime(s.Position)+" / "+mediaTime(s.Duration), pale, true)

	if s.Message != "" || !s.Loaded {
		message := s.Message
		if message == "" {
			message = "Opening video…"
		}
		r.label(image.Rect(l.Video.Min.X+28, l.Video.Min.Y+l.Video.Dy()/2, l.Video.Max.X-28, l.Video.Max.Y-18), message, 0x9cdae0, false)
	} else if s.VideoWidth <= 0 || s.VideoHeight <= 0 {
		r.label(image.Rect(l.Video.Min.X+28, l.Video.Min.Y+l.Video.Dy()/2, l.Video.Max.X-28, l.Video.Max.Y-18), "Audio playback · no video stream", 0x9cdae0, false)
	} else if s.Paused || s.Ended {
		p = center(l.Video)
		// A dark disc keeps the three broken concentric rings readable
		// against arbitrary paused frames. It disappears completely during play.
		r.arc(0x092b3b, p, 42, 42, 0, 2*math.Pi)
		r.arc(0x174d5d, p, 48, 1, 0, 2*math.Pi)
		for i := 0; i < 3; i++ {
			a := float64(i) * 2 * math.Pi / 3
			r.arc(cyan, p, 45, 1.5, a+.07, a+1.91)
			r.arc(0x43b9c9, p, 39, 2, a+.32, a+1.95)
		}
		r.polygon(pale, image.Pt(p.X-9, p.Y-17), image.Pt(p.X+18, p.Y), image.Pt(p.X-9, p.Y+17))
	}
	footer := "SPACE  PLAY / PAUSE     ← / →  SEEK 10s     M  MUTE"
	if s.VideoWidth > 0 && w >= 900 {
		footer += fmt.Sprintf("     %d × %d", s.VideoWidth, s.VideoHeight)
	}
	r.label(image.Rect(73, h-20, w-65, h-4), footer, 0x5393a6, true)
	return r.texture.Replace(r.image.Rect.Dx(), r.image.Rect.Dy(), r.image.Pix)
}
