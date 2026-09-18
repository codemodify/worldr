package photoapp

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"unicode"

	"github.com/codemodify/worldr/internal/render"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// These bounds apply to the requested display box. A photo's tightly fitted
// texture can be smaller in either dimension, down to one raster pixel.
const minWidth, minHeight, maxWidth, maxHeight = 640, 400, 1920, 1080

type photoRenderer struct {
	image     *image.RGBA
	texture   *render.Texture
	requested image.Point
	face      font.Face
	closed    bool
}

func newPhotoRenderer(width, height int) (*photoRenderer, error) {
	r := &photoRenderer{}
	if err := r.resize(width, height); err != nil {
		return nil, err
	}
	parsed, err := opentype.Parse(gomono.TTF)
	if err != nil {
		return nil, err
	}
	r.face, err = opentype.NewFace(parsed, &opentype.FaceOptions{Size: 16, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	if err := r.paint("", true, nil); err != nil {
		r.close()
		return nil, err
	}
	return r, nil
}

func (r *photoRenderer) close() {
	if r.closed {
		return
	}
	r.closed = true
	if r.face != nil {
		_ = r.face.Close()
	}
	r.face, r.image = nil, nil
}

// resize records the box independently of the current texture size. The next
// paint fits the full image into it; a portrait texture must not become the
// requested box for a later landscape replacement or trigger repeated resizes.
func (r *photoRenderer) resize(width, height int) error {
	if r.closed {
		return fmt.Errorf("photo renderer is closed")
	}
	if width < minWidth || height < minHeight || width > maxWidth || height > maxHeight {
		return fmt.Errorf("photo display box must be between 640x400 and 1920x1080")
	}
	r.requested = image.Pt(width, height)
	return nil
}

func fitPhotoSize(box image.Point, source image.Rectangle) image.Point {
	if source.Empty() {
		return box
	}
	scale := min(float64(box.X)/float64(source.Dx()), float64(box.Y)/float64(source.Dy()))
	// Integer texture dimensions introduce at most half a pixel of rounding on
	// the unconstrained side. Every source edge is sampled, including panoramas.
	return image.Pt(max(1, min(box.X, int(math.Round(float64(source.Dx())*scale)))), max(1, min(box.Y, int(math.Round(float64(source.Dy())*scale)))))
}

func (r *photoRenderer) paint(message string, loading bool, source image.Image) error {
	if r.closed {
		return fmt.Errorf("photo renderer is closed")
	}
	size := r.requested
	if source != nil {
		if source.Bounds().Empty() {
			return fmt.Errorf("image has no pixels")
		}
		size = fitPhotoSize(r.requested, source.Bounds())
	}
	if r.image == nil || r.image.Rect.Size() != size {
		r.image = image.NewRGBA(image.Rectangle{Max: size})
	}
	if source != nil {
		// The entire texture is the photograph. Src avoids a full-source opacity
		// scan; transparent pixels are composited over a checker in place below.
		xdraw.ApproxBiLinear.Scale(r.image, r.image.Rect, source, source.Bounds(), draw.Src, nil)
		for y := 0; y < size.Y; y++ {
			for x := 0; x < size.X; x++ {
				i := r.image.PixOffset(x, y)
				a := uint32(255 - r.image.Pix[i+3])
				if a == 0 {
					continue
				}
				c := uint32(34)
				if (x/8+y/8)&1 != 0 {
					c = 44
				}
				r.image.Pix[i] += byte((c*a + 127) / 255)
				r.image.Pix[i+1] += byte(((c+5)*a + 127) / 255)
				r.image.Pix[i+2] += byte(((c+9)*a + 127) / 255)
				r.image.Pix[i+3] = 255
			}
		}
		// Pending replacements do not decorate or obscure the existing photo.
		if message != "" {
			r.status(message, false)
		}
	} else {
		draw.Draw(r.image, r.image.Rect, image.NewUniform(color.RGBA{8, 20, 33, 255}), image.Point{}, draw.Src)
		if message == "" {
			message = "No image selected"
			if loading {
				message = "Opening image…"
			}
		}
		r.status(message, true)
	}
	if r.texture == nil {
		var err error
		r.texture, err = render.NewTexture(size.X, size.Y, r.image.Pix)
		return err
	}
	return r.texture.Replace(size.X, size.Y, r.image.Pix)
}

// status is used only for loading/failure, never for healthy image controls,
// filename, borders, or telemetry. Long errors are clipped to the image bounds.
func (r *photoRenderer) status(message string, centered bool) {
	var clean strings.Builder
	for _, c := range message {
		if !unicode.IsControl(c) && !unicode.Is(unicode.Cf, c) {
			clean.WriteRune(c)
		}
		if clean.Len() >= 2048 {
			break
		}
	}
	b := r.image.Rect
	height := r.face.Metrics().Height.Ceil() + 20
	y := max(0, b.Max.Y-height)
	if centered {
		y = max(0, (b.Max.Y-height)/2)
	} else {
		draw.Draw(r.image, image.Rect(0, y, b.Max.X, b.Max.Y), image.NewUniform(color.RGBA{8, 20, 33, 240}), image.Point{}, draw.Over)
	}
	text := []rune(clean.String())
	available := max(0, b.Dx()-32)
	for len(text) > 0 && font.MeasureString(r.face, string(text)).Ceil() > available {
		text = text[:len(text)-1]
	}
	x := 16
	if centered {
		x = max(0, (b.Dx()-font.MeasureString(r.face, string(text)).Ceil())/2)
	}
	d := font.Drawer{Dst: r.image, Src: image.NewUniform(color.RGBA{190, 218, 226, 255}), Face: r.face, Dot: fixed.P(x, y+10+r.face.Metrics().Ascent.Ceil())}
	d.DrawString(string(text))
}
