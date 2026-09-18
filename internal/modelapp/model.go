// Package modelapp hosts file-backed model inspection as native scene objects.
package modelapp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/codemodify/worldr/internal/scene"
)

const maxModelBytes = 32 << 20
const maxTriangles = 200000

type Component struct {
	Name      string
	Mesh      *scene.Mesh
	Triangles int
}
type Model struct {
	Components       []Component
	Minimum, Maximum scene.Vec3
	Triangles        int
}

func IsModelPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".worldr-model.json") || strings.EqualFold(filepath.Ext(path), ".obj") || strings.EqualFold(filepath.Ext(path), ".stl")
}

func readModel(ctx context.Context, file *os.File, name string) (*Model, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxModelBytes {
		return nil, fmt.Errorf("model must be a regular file no larger than 32 MiB")
	}
	data := make([]byte, info.Size())
	for off := 0; off < len(data); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := file.Read(data[off:min(off+65536, len(data))])
		off += n
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, fmt.Errorf("model read made no progress")
		}
	}
	if strings.EqualFold(filepath.Ext(name), ".stl") {
		return parseSTL(ctx, data)
	}
	if !strings.EqualFold(filepath.Ext(name), ".obj") {
		return nil, fmt.Errorf("supported model formats are triangle OBJ and STL")
	}
	return parseOBJ(ctx, data)
}

type modelBuilder struct {
	vertices  []scene.Vec3
	indices   []uint32
	name      string
	model     Model
	hasBounds bool
}

func (b *modelBuilder) vertex(p scene.Vec3) error {
	for _, v := range []float32{p.X, p.Y, p.Z} {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || math.Abs(float64(v)) > 1e12 {
			return fmt.Errorf("model coordinates must be finite and within range")
		}
	}
	if len(b.vertices) >= maxTriangles*3 {
		return fmt.Errorf("model exceeds vertex budget")
	}
	b.vertices = append(b.vertices, p)
	return nil
}
func (b *modelBuilder) flush() error {
	if len(b.indices) == 0 {
		return nil
	}
	if len(b.model.Components) >= 256 {
		return fmt.Errorf("model exceeds 256 components")
	}
	vertices := make([]scene.Vec3, 0, len(b.indices))
	indices := make([]uint32, len(b.indices))
	remap := make(map[uint32]uint32)
	for i, old := range b.indices {
		index, ok := remap[old]
		if !ok {
			index = uint32(len(vertices))
			vertices = append(vertices, b.vertices[old])
			remap[old] = index
		}
		indices[i] = index
	}
	mesh, err := scene.NewMesh(vertices, indices, nil)
	if err != nil {
		return err
	}
	name := b.name
	if name == "" {
		name = fmt.Sprintf("Component %d", len(b.model.Components)+1)
	}
	b.model.Components = append(b.model.Components, Component{name, mesh, len(b.indices) / 3})
	b.indices = nil
	return nil
}
func (b *modelBuilder) triangle(a, c, d uint32) error {
	if b.model.Triangles >= maxTriangles {
		return fmt.Errorf("model exceeds 200000 triangles")
	}
	for _, index := range []uint32{a, c, d} {
		p := b.vertices[index]
		if !b.hasBounds {
			b.model.Minimum, b.model.Maximum = p, p
			b.hasBounds = true
		} else {
			b.model.Minimum = scene.Vec3{X: min(b.model.Minimum.X, p.X), Y: min(b.model.Minimum.Y, p.Y), Z: min(b.model.Minimum.Z, p.Z)}
			b.model.Maximum = scene.Vec3{X: max(b.model.Maximum.X, p.X), Y: max(b.model.Maximum.Y, p.Y), Z: max(b.model.Maximum.Z, p.Z)}
		}
	}
	b.indices = append(b.indices, a, c, d)
	b.model.Triangles++
	return nil
}
func (b *modelBuilder) finish() (*Model, error) {
	if err := b.flush(); err != nil {
		return nil, err
	}
	if b.model.Triangles == 0 {
		return nil, fmt.Errorf("model contains no triangles")
	}
	if b.model.Maximum.Sub(b.model.Minimum).Length() < 1e-8 {
		return nil, fmt.Errorf("model has zero extent")
	}
	return &b.model, nil
}
func parseOBJ(ctx context.Context, data []byte) (*Model, error) {
	b := modelBuilder{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 65536)
	line := 0
	for scanner.Scan() {
		line++
		if line%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		text := strings.SplitN(scanner.Text(), "#", 2)[0]
		f := strings.Fields(text)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "v":
			if len(f) < 4 {
				return nil, fmt.Errorf("OBJ line %d: vertex requires xyz", line)
			}
			var p [3]float32
			for i := range p {
				v, err := strconv.ParseFloat(f[i+1], 32)
				if err != nil {
					return nil, fmt.Errorf("OBJ line %d: %w", line, err)
				}
				p[i] = float32(v)
			}
			if err := b.vertex(scene.Vec3{X: p[0], Y: p[1], Z: p[2]}); err != nil {
				return nil, err
			}
		case "o", "g":
			if err := b.flush(); err != nil {
				return nil, err
			}
			b.name = strings.Join(f[1:], " ")
			if len(b.name) > 128 {
				b.name = b.name[:128]
			}
		case "f":
			if len(f) != 4 {
				return nil, fmt.Errorf("OBJ line %d: export triangulated faces before opening", line)
			}
			var indices [3]uint32
			for i := range indices {
				raw := strings.SplitN(f[i+1], "/", 2)[0]
				index, err := strconv.Atoi(raw)
				if err != nil {
					return nil, fmt.Errorf("OBJ line %d: invalid face", line)
				}
				if index < 0 {
					index = len(b.vertices) + index + 1
				}
				if index < 1 || index > len(b.vertices) {
					return nil, fmt.Errorf("OBJ line %d: face index outside vertices", line)
				}
				indices[i] = uint32(index - 1)
			}
			if err := b.triangle(indices[0], indices[1], indices[2]); err != nil {
				return nil, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return b.finish()
}
func parseSTL(ctx context.Context, data []byte) (*Model, error) {
	b := modelBuilder{name: "STL mesh"}
	if len(data) >= 84 && uint64(binary.LittleEndian.Uint32(data[80:84]))*50+84 == uint64(len(data)) {
		count := int(binary.LittleEndian.Uint32(data[80:84]))
		if count > maxTriangles {
			return nil, fmt.Errorf("STL exceeds triangle budget")
		}
		for face := 0; face < count; face++ {
			if face%256 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			base := uint32(len(b.vertices))
			offset := 84 + face*50 + 12
			for corner := 0; corner < 3; corner++ {
				var v [3]float32
				for axis := range v {
					at := offset + corner*12 + axis*4
					v[axis] = math.Float32frombits(binary.LittleEndian.Uint32(data[at : at+4]))
				}
				if err := b.vertex(scene.Vec3{X: v[0], Y: v[1], Z: v[2]}); err != nil {
					return nil, err
				}
			}
			if err := b.triangle(base, base+1, base+2); err != nil {
				return nil, err
			}
		}
	} else {
		scanner := bufio.NewScanner(bytes.NewReader(data))
		scanner.Buffer(make([]byte, 4096), 65536)
		facet := false
		count := 0
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			f := strings.Fields(scanner.Text())
			if len(f) == 0 {
				continue
			}
			switch f[0] {
			case "facet":
				if facet {
					return nil, fmt.Errorf("nested STL facet")
				}
				facet = true
				count = 0
			case "vertex":
				if !facet || len(f) != 4 || count >= 3 {
					return nil, fmt.Errorf("invalid STL vertex")
				}
				var v [3]float32
				for i := range v {
					n, err := strconv.ParseFloat(f[i+1], 32)
					if err != nil {
						return nil, err
					}
					v[i] = float32(n)
				}
				if err := b.vertex(scene.Vec3{X: v[0], Y: v[1], Z: v[2]}); err != nil {
					return nil, err
				}
				count++
			case "endfacet":
				if !facet || count != 3 {
					return nil, fmt.Errorf("STL facet requires three vertices")
				}
				base := uint32(len(b.vertices) - 3)
				if err := b.triangle(base, base+1, base+2); err != nil {
					return nil, err
				}
				facet = false
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		if facet {
			return nil, fmt.Errorf("unfinished STL facet")
		}
	}
	return b.finish()
}
