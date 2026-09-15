package engine

// Glyph is a 5×7 bitmap (bit 0 = leftmost pixel of each row).
type Glyph [7]byte

const (
	glyphW = 5
	glyphH = 7
)

// TextWidth is the pixel width of s at the given integer scale (≥1).
func TextWidth(s string, scale int) int {
	if scale < 1 {
		scale = 1
	}
	n := 0
	for range s {
		n++
	}
	if n == 0 {
		return 0
	}
	return n*(glyphW+1)*scale - scale
}

// TextHeight is the pixel height of a line at scale.
func TextHeight(scale int) int {
	if scale < 1 {
		scale = 1
	}
	return glyphH * scale
}

// DrawText blits a 5×7 bitmap string in pixel (BGRA). Unknown runes are a box.
func DrawText(dst []byte, stride, dW, dH, x, y int, s string, pixel uint32, scale int) {
	if scale < 1 {
		scale = 1
	}
	cx := x
	for _, r := range s {
		g := lookupGlyph(r)
		drawGlyph(dst, stride, dW, dH, cx, y, g, pixel, scale)
		cx += (glyphW + 1) * scale
	}
}

func drawGlyph(dst []byte, stride, dW, dH, x, y int, g Glyph, pixel uint32, scale int) {
	b0 := byte(pixel)
	b1 := byte(pixel >> 8)
	b2 := byte(pixel >> 16)
	b3 := byte(pixel >> 24)
	for row := 0; row < glyphH; row++ {
		bits := g[row]
		for col := 0; col < glyphW; col++ {
			if bits&(1<<col) == 0 {
				continue
			}
			px := x + col*scale
			py := y + row*scale
			for yy := 0; yy < scale; yy++ {
				iy := py + yy
				if iy < 0 || iy >= dH {
					continue
				}
				for xx := 0; xx < scale; xx++ {
					ix := px + xx
					if ix < 0 || ix >= dW {
						continue
					}
					i := iy*stride + ix*4
					if i+4 > len(dst) {
						continue
					}
					dst[i+0] = b0
					dst[i+1] = b1
					dst[i+2] = b2
					dst[i+3] = b3
				}
			}
		}
	}
}

func lookupGlyph(r rune) Glyph {
	if r >= 0 && int(r) < len(font5x7) {
		if g := font5x7[r]; g != (Glyph{}) || r == ' ' {
			return g
		}
	}
	return glyphBox
}

var glyphBox = Glyph{0x1f, 0x11, 0x11, 0x11, 0x11, 0x11, 0x1f}

// font5x7 covers ASCII 32–126. Empty slots fall back to glyphBox.
var font5x7 [127]Glyph

func init() {
	// Each string is 7 rows of 5 chars; '#' is on.
	put := func(r rune, rows [7]string) {
		var g Glyph
		for i, row := range rows {
			var b byte
			for c := 0; c < 5 && c < len(row); c++ {
				if row[c] != '.' && row[c] != ' ' && row[c] != '0' {
					b |= 1 << uint(c)
				}
			}
			g[i] = b
		}
		font5x7[r] = g
	}
	put(' ', [7]string{".....", ".....", ".....", ".....", ".....", ".....", "....."})
	put('!', [7]string{"..#..", "..#..", "..#..", "..#..", ".....", ".....", "..#.."})
	put('#', [7]string{".#.#.", "#####", ".#.#.", ".#.#.", "#####", ".#.#.", "....."})
	put('+', [7]string{".....", "..#..", "..#..", "#####", "..#..", "..#..", "....."})
	put('-', [7]string{".....", ".....", ".....", "#####", ".....", ".....", "....."})
	put('.', [7]string{".....", ".....", ".....", ".....", ".....", ".....", "..#.."})
	put('/', [7]string{"....#", "...#.", "..#..", ".#...", "#....", ".....", "....."})
	put(':', [7]string{".....", "..#..", ".....", ".....", ".....", "..#..", "....."})
	put('0', [7]string{".###.", "#...#", "#..##", "#.#.#", "##..#", "#...#", ".###."})
	put('1', [7]string{"..#..", ".##..", "..#..", "..#..", "..#..", "..#..", ".###."})
	put('2', [7]string{".###.", "#...#", "....#", "...#.", "..#..", ".#...", "#####"})
	put('3', [7]string{".###.", "#...#", "....#", "..##.", "....#", "#...#", ".###."})
	put('4', [7]string{"...#.", "..##.", ".#.#.", "#..#.", "#####", "...#.", "...#."})
	put('5', [7]string{"#####", "#....", "####.", "....#", "....#", "#...#", ".###."})
	put('6', [7]string{".###.", "#....", "#....", "####.", "#...#", "#...#", ".###."})
	put('7', [7]string{"#####", "....#", "...#.", "..#..", ".#...", ".#...", ".#..."})
	put('8', [7]string{".###.", "#...#", "#...#", ".###.", "#...#", "#...#", ".###."})
	put('9', [7]string{".###.", "#...#", "#...#", ".####", "....#", "....#", ".###."})
	letters := []struct {
		r    rune
		rows [7]string
	}{
		{'A', [7]string{".###.", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"}},
		{'B', [7]string{"####.", "#...#", "#...#", "####.", "#...#", "#...#", "####."}},
		{'C', [7]string{".###.", "#...#", "#....", "#....", "#....", "#...#", ".###."}},
		{'D', [7]string{"####.", "#...#", "#...#", "#...#", "#...#", "#...#", "####."}},
		{'E', [7]string{"#####", "#....", "#....", "####.", "#....", "#....", "#####"}},
		{'F', [7]string{"#####", "#....", "#....", "####.", "#....", "#....", "#...."}},
		{'G', [7]string{".###.", "#...#", "#....", "#.###", "#...#", "#...#", ".###."}},
		{'H', [7]string{"#...#", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"}},
		{'I', [7]string{".###.", "..#..", "..#..", "..#..", "..#..", "..#..", ".###."}},
		{'J', [7]string{"..###", "...#.", "...#.", "...#.", "...#.", "#..#.", ".##.."}},
		{'K', [7]string{"#...#", "#..#.", "#.#..", "##...", "#.#..", "#..#.", "#...#"}},
		{'L', [7]string{"#....", "#....", "#....", "#....", "#....", "#....", "#####"}},
		{'M', [7]string{"#...#", "##.##", "#.#.#", "#...#", "#...#", "#...#", "#...#"}},
		{'N', [7]string{"#...#", "##..#", "#.#.#", "#..##", "#...#", "#...#", "#...#"}},
		{'O', [7]string{".###.", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."}},
		{'P', [7]string{"####.", "#...#", "#...#", "####.", "#....", "#....", "#...."}},
		{'Q', [7]string{".###.", "#...#", "#...#", "#...#", "#.#.#", "#..#.", "..##."}},
		{'R', [7]string{"####.", "#...#", "#...#", "####.", "#.#..", "#..#.", "#...#"}},
		{'S', [7]string{".####", "#....", "#....", ".###.", "....#", "....#", "####."}},
		{'T', [7]string{"#####", "..#..", "..#..", "..#..", "..#..", "..#..", "..#.."}},
		{'U', [7]string{"#...#", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."}},
		{'V', [7]string{"#...#", "#...#", "#...#", "#...#", "#...#", ".#.#.", "..#.."}},
		{'W', [7]string{"#...#", "#...#", "#...#", "#.#.#", "#.#.#", "#.#.#", ".#.#."}},
		{'X', [7]string{"#...#", "#...#", ".#.#.", "..#..", ".#.#.", "#...#", "#...#"}},
		{'Y', [7]string{"#...#", "#...#", ".#.#.", "..#..", "..#..", "..#..", "..#.."}},
		{'Z', [7]string{"#####", "....#", "...#.", "..#..", ".#...", "#....", "#####"}},
	}
	for _, L := range letters {
		put(L.r, L.rows)
		// lowercase shares the uppercase bitmap (panel titles stay readable).
		if L.r >= 'A' && L.r <= 'Z' {
			put(L.r+32, L.rows)
		}
	}
	put('_', [7]string{".....", ".....", ".....", ".....", ".....", ".....", "#####"})
}
