package fluid

import "math"

// Spring retains presentation position independently from the target layout.
// Velocity is measured in coordinate units/second. Spring motion never changes
// a panel's logical membership or joins the movement of neighboring panels.
type Spring struct{ Position, Velocity Point }

// Reset places the spring at p with no momentum. Nonfinite input is ignored.
func (s *Spring) Reset(p Point) {
	if s != nil && finite(p.X, p.Y) {
		s.Position, s.Velocity = p, Point{}
	}
}

// Step analytically advances a damped spring toward a stationary target over
// dt seconds. Frequency is in Hz (0,120], damping is a ratio in [0,4], and dt
// is in (0,10]. Damping=1 is critical; values below it may overshoot. Invalid
// inputs are ignored, and dt=0 is a no-op. The closed-form update avoids the
// frame-rate instability of an Euler step. Use Reset for reduced motion.
func (s *Spring) Step(target Point, dt, frequency, damping float32) {
	if s == nil || !finite(target.X, target.Y, s.Position.X, s.Position.Y, s.Velocity.X, s.Velocity.Y, dt, frequency, damping) || dt <= 0 || dt > 10 || frequency <= 0 || frequency > 120 || damping < 0 || damping > 4 {
		return
	}
	w, z, t := 2*math.Pi*float64(frequency), float64(damping), float64(dt)
	step := func(position, velocity, target float32) (float32, float32) {
		x, v := float64(position-target), float64(velocity)
		var next, speed float64
		switch {
		case math.Abs(z-1) < 1e-5:
			e := math.Exp(-w * t)
			q := v + w*x
			next, speed = (x+q*t)*e, (v-w*q*t)*e
		case z < 1:
			wd := w * math.Sqrt(1-z*z)
			e := math.Exp(-z * w * t)
			sn, cs := math.Sincos(wd * t)
			q := (v + z*w*x) / wd
			next = e * (x*cs + q*sn)
			speed = e * ((q*wd-z*w*x)*cs + (-x*wd-z*w*q)*sn)
		default:
			r := math.Sqrt(z*z - 1)
			r1, r2 := -w*(z-r), -w*(z+r)
			c1 := (v - r2*x) / (r1 - r2)
			c2 := x - c1
			e1, e2 := c1*math.Exp(r1*t), c2*math.Exp(r2*t)
			next, speed = e1+e2, r1*e1+r2*e2
		}
		return float32(float64(target) + next), float32(speed)
	}
	x, vx := step(s.Position.X, s.Velocity.X, target.X)
	y, vy := step(s.Position.Y, s.Velocity.Y, target.Y)
	if finite(x, y, vx, vy) {
		s.Position, s.Velocity = Point{x, y}, Point{vx, vy}
	}
}

// SnapGrid rounds each coordinate to the nearest grid multiple. Halfway values
// round away from zero. Invalid spacing or nonfinite input leaves p unchanged.
func SnapGrid(p Point, spacing float32) Point {
	if !finite(p.X, p.Y, spacing) || spacing <= 0 || spacing > MaxDimension {
		return p
	}
	x := float32(math.Round(float64(p.X)/float64(spacing)) * float64(spacing))
	y := float32(math.Round(float64(p.Y)/float64(spacing)) * float64(spacing))
	if !finite(x, y) {
		return p
	}
	return Point{x, y}
}

func intervalGap(a, lengthA, b, lengthB float32) float32 {
	return max(max(a-b-lengthB, b-a-lengthA), 0)
}

// SnapMagnetic aligns the nearest edges or centers, or docks adjacent edges,
// independently on each axis. Peers must also overlap or be within distance
// along the other axis. Ties follow peer order. The bool reports a matched
// target, including an already aligned edge. Sizes are never changed.
// Invalid bounds/distance or more than MaxSurfaces peers return unchanged;
// invalid individual peers are skipped. Apply ClampRect afterward if needed.
func SnapMagnetic(bounds Rect, peers []Rect, distance float32) (Rect, bool) {
	if bounds.Validate() != nil || !finite(distance) || distance < 0 || distance > MaxBlend || len(peers) > MaxSurfaces {
		return bounds, false
	}
	dx, dy := float32(0), float32(0)
	bestX, bestY := float32(math.Inf(1)), float32(math.Inf(1))
	snapX, snapY := false, false
	for _, peer := range peers {
		if peer.Validate() != nil {
			continue
		}
		if intervalGap(bounds.Y, bounds.Height, peer.Y, peer.Height) <= distance {
			for _, delta := range [...]float32{peer.X - bounds.X, peer.X + peer.Width/2 - bounds.X - bounds.Width/2, peer.X + peer.Width - bounds.X - bounds.Width, peer.X + peer.Width - bounds.X, peer.X - bounds.X - bounds.Width} {
				if magnitude := abs(delta); magnitude <= distance && magnitude < bestX {
					dx, bestX, snapX = delta, magnitude, true
				}
			}
		}
		if intervalGap(bounds.X, bounds.Width, peer.X, peer.Width) <= distance {
			for _, delta := range [...]float32{peer.Y - bounds.Y, peer.Y + peer.Height/2 - bounds.Y - bounds.Height/2, peer.Y + peer.Height - bounds.Y - bounds.Height, peer.Y + peer.Height - bounds.Y, peer.Y - bounds.Y - bounds.Height} {
				if magnitude := abs(delta); magnitude <= distance && magnitude < bestY {
					dy, bestY, snapY = delta, magnitude, true
				}
			}
		}
	}
	result := bounds
	result.X += dx
	result.Y += dy
	if result.Validate() != nil {
		return bounds, false
	}
	return result, snapX || snapY
}

// ClampRect fits bounds inside container, reducing oversize dimensions only
// when necessary. Invalid input leaves bounds unchanged.
func ClampRect(bounds, container Rect) Rect {
	if bounds.Validate() != nil || container.Validate() != nil {
		return bounds
	}
	bounds.Width, bounds.Height = min(bounds.Width, container.Width), min(bounds.Height, container.Height)
	bounds.X = max(container.X, min(bounds.X, container.X+container.Width-bounds.Width))
	bounds.Y = max(container.Y, min(bounds.Y, container.Y+container.Height-bounds.Height))
	return bounds
}
