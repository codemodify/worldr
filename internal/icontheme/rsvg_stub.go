//go:build !librsvg || !cgo || !linux

package icontheme

import "fmt"

// RsvgAvailable is false unless the binary was built with linux+cgo+-tags=librsvg.
func RsvgAvailable() bool { return false }

// RasterRSVG is the stub when librsvg headers were not linked.
func RasterRSVG(path string, want int) (pix []byte, w, h, stride int, err error) {
	return nil, 0, 0, 0, fmt.Errorf("librsvg unavailable (need linux + CGO + librsvg2-dev + -tags=librsvg)")
}
