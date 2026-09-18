// Package scene provides native drawable objects, hierarchical geometry, and
// interaction without depending on an application-window model.
package scene

import "math"

type Vec3 struct{ X, Y, Z float32 }

func (a Vec3) Add(b Vec3) Vec3    { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a Vec3) Sub(b Vec3) Vec3    { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec3) Mul(s float32) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }
func (a Vec3) Dot(b Vec3) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}
func (a Vec3) Length() float32 {
	return float32(math.Sqrt(float64(a.X)*float64(a.X) + float64(a.Y)*float64(a.Y) + float64(a.Z)*float64(a.Z)))
}
func (a Vec3) Normalize() Vec3 {
	if length := a.Length(); length > 0 {
		return a.Mul(1 / length)
	}
	return Vec3{}
}

// Mat4 is column-major. a.Mul(b) applies b first, then a.
type Mat4 [16]float32

func Identity() Mat4 { return Mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1} }
func Translate(x, y, z float32) Mat4 {
	m := Identity()
	m[12], m[13], m[14] = x, y, z
	return m
}
func Scale(x, y, z float32) Mat4 { return Mat4{x, 0, 0, 0, 0, y, 0, 0, 0, 0, z, 0, 0, 0, 0, 1} }
func RotateX(angle float32) Mat4 {
	c, s := float32(math.Cos(float64(angle))), float32(math.Sin(float64(angle)))
	return Mat4{1, 0, 0, 0, 0, c, s, 0, 0, -s, c, 0, 0, 0, 0, 1}
}
func RotateY(angle float32) Mat4 {
	c, s := float32(math.Cos(float64(angle))), float32(math.Sin(float64(angle)))
	return Mat4{c, 0, -s, 0, 0, 1, 0, 0, s, 0, c, 0, 0, 0, 0, 1}
}
func RotateZ(angle float32) Mat4 {
	c, s := float32(math.Cos(float64(angle))), float32(math.Sin(float64(angle)))
	return Mat4{c, s, 0, 0, -s, c, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
}
func (a Mat4) Mul(b Mat4) Mat4 {
	var out Mat4
	for col := 0; col < 4; col++ {
		for row := 0; row < 4; row++ {
			for k := 0; k < 4; k++ {
				out[col*4+row] += a[k*4+row] * b[col*4+k]
			}
		}
	}
	return out
}
func (m Mat4) TransformPoint(p Vec3) Vec3 {
	v := m.transform(p)
	if v.w != 0 && v.w != 1 {
		return Vec3{v.x / v.w, v.y / v.w, v.z / v.w}
	}
	return Vec3{v.x, v.y, v.z}
}

func (m Mat4) TransformVector(v Vec3) Vec3 {
	return Vec3{m[0]*v.X + m[4]*v.Y + m[8]*v.Z, m[1]*v.X + m[5]*v.Y + m[9]*v.Z, m[2]*v.X + m[6]*v.Y + m[10]*v.Z}
}

// Inverse is used for input rays, not for per-vertex frame preparation.
func (m Mat4) Inverse() (Mat4, bool) {
	var a [4][8]float64
	for row := 0; row < 4; row++ {
		for col := 0; col < 4; col++ {
			a[row][col] = float64(m[col*4+row])
		}
		a[row][row+4] = 1
	}
	for col := 0; col < 4; col++ {
		pivot := col
		for row := col + 1; row < 4; row++ {
			if math.Abs(a[row][col]) > math.Abs(a[pivot][col]) {
				pivot = row
			}
		}
		if math.Abs(a[pivot][col]) < 1e-14 {
			return Mat4{}, false
		}
		a[col], a[pivot] = a[pivot], a[col]
		divisor := a[col][col]
		for j := 0; j < 8; j++ {
			a[col][j] /= divisor
		}
		for row := 0; row < 4; row++ {
			if row != col {
				factor := a[row][col]
				for j := 0; j < 8; j++ {
					a[row][j] -= factor * a[col][j]
				}
			}
		}
	}
	var inverse Mat4
	for row := 0; row < 4; row++ {
		for col := 0; col < 4; col++ {
			inverse[col*4+row] = float32(a[row][col+4])
		}
	}
	return inverse, true
}

type vec4 struct{ x, y, z, w float32 }

func (m Mat4) transform(p Vec3) vec4 {
	return vec4{
		m[0]*p.X + m[4]*p.Y + m[8]*p.Z + m[12],
		m[1]*p.X + m[5]*p.Y + m[9]*p.Z + m[13],
		m[2]*p.X + m[6]*p.Y + m[10]*p.Z + m[14],
		m[3]*p.X + m[7]*p.Y + m[11]*p.Z + m[15],
	}
}

type Viewport struct{ X, Y, Width, Height float32 }

// Camera uses a right-handed world, radians for FOV, and a Vulkan [0,1]
// depth range. Zero FOV/Near/Far/Up choose useful defaults.
type Camera struct {
	Eye, Target, Up Vec3
	FOV, Near, Far  float32
}

func (c Camera) matrix(vp Viewport) Mat4 {
	fov, near, far := c.FOV, c.Near, c.Far
	if fov <= 0 || fov >= math.Pi {
		fov = math.Pi / 4
	}
	if near <= 0 {
		near = 0.05
	}
	if far <= near {
		far = 1000
	}
	up := c.Up.Normalize()
	if up == (Vec3{}) {
		up = Vec3{0, 1, 0}
	}
	forward := c.Target.Sub(c.Eye).Normalize()
	if forward == (Vec3{}) {
		forward = Vec3{0, 0, -1}
	}
	right := forward.Cross(up).Normalize()
	if right == (Vec3{}) {
		up = Vec3{0, 0, 1}
		if abs(forward.Z) > 0.99 {
			up = Vec3{1, 0, 0}
		}
		right = forward.Cross(up).Normalize()
	}
	up = right.Cross(forward)
	view := Mat4{
		right.X, up.X, -forward.X, 0,
		right.Y, up.Y, -forward.Y, 0,
		right.Z, up.Z, -forward.Z, 0,
		-right.Dot(c.Eye), -up.Dot(c.Eye), forward.Dot(c.Eye), 1,
	}
	f := 1 / float32(math.Tan(float64(fov/2)))
	projection := Mat4{
		f * vp.Height / vp.Width, 0, 0, 0,
		0, -f, 0, 0,
		0, 0, far / (near - far), -1,
		0, 0, far * near / (near - far), 0,
	}
	return projection.Mul(view)
}

// Project returns framebuffer coordinates and normalized depth. visible means
// the point lies inside the camera frustum, including its near and far planes.
func (c Camera) Project(point Vec3, vp Viewport) (x, y, depth float32, visible bool) {
	if vp.Width <= 0 || vp.Height <= 0 {
		return
	}
	p := c.matrix(vp).transform(point)
	if p.w <= 0 {
		return
	}
	x, y, depth = vp.X+(p.x/p.w+1)*vp.Width/2, vp.Y+(p.y/p.w+1)*vp.Height/2, p.z/p.w
	visible = p.x >= -p.w && p.x <= p.w && p.y >= -p.w && p.y <= p.w && p.z >= 0 && p.z <= p.w
	return
}

// Ray starts on the camera's near plane. Direction is normalized in world
// space; MaxDistance ends at the far plane so picking shares GPU clipping.
type Ray struct {
	Origin, Direction Vec3
	MaxDistance       float32
}

func (c Camera) Ray(vp Viewport, x, y float32) (Ray, bool) {
	return c.pointerRay(vp, x, y, false)
}

func (c Camera) pointerRay(vp Viewport, x, y float32, captured bool) (Ray, bool) {
	if vp.Width <= 0 || vp.Height <= 0 || !finite(vp.X) || !finite(vp.Y) || !finite(vp.Width) || !finite(vp.Height) || !finite(x) || !finite(y) {
		return Ray{}, false
	}
	if !captured && (x < vp.X || y < vp.Y || x >= vp.X+vp.Width || y >= vp.Y+vp.Height) {
		return Ray{}, false
	}
	inverse, ok := c.matrix(vp).Inverse()
	if !ok {
		return Ray{}, false
	}
	nx, ny := 2*(x-vp.X)/vp.Width-1, 2*(y-vp.Y)/vp.Height-1
	near, far := inverse.TransformPoint(Vec3{nx, ny, 0}), inverse.TransformPoint(Vec3{nx, ny, 1})
	delta := far.Sub(near)
	length := delta.Length()
	if length <= 0 {
		return Ray{}, false
	}
	return Ray{Origin: near, Direction: delta.Mul(1 / length), MaxDistance: length}, true
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
