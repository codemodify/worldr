package workspace

import "time"

const navigationMotionDuration = 260 * time.Millisecond

// A hidden pose still has a positive box: it is the collapse destination or
// expansion origin for that slot. Slots retain their application identities.
type navigationPose struct {
	bounds  box
	visible bool
}

// navigationMotion interpolates presentation only; it never changes document
// placement, application selection, focus, stacking or input ownership.
type navigationMotion struct {
	current, start, target [MaxApplicationLayouts]navigationPose
	initialized            bool
	elapsed                time.Duration
}

func (m *navigationMotion) snap(target [MaxApplicationLayouts]navigationPose) {
	m.current, m.start, m.target = target, target, target
	m.initialized, m.elapsed = true, navigationMotionDuration
}

func (m *navigationMotion) retarget(target [MaxApplicationLayouts]navigationPose) {
	if !m.initialized {
		m.snap(target)
		return
	}
	if m.target == target {
		return
	}
	if m.current == target {
		m.snap(target)
		return
	}
	m.start, m.target, m.elapsed = m.current, target, 0
	for i := range m.current {
		m.current[i].visible = m.start[i].visible || target[i].visible
	}
}

func (m *navigationMotion) moving() bool {
	return m.initialized && m.elapsed < navigationMotionDuration
}

func (m *navigationMotion) poses() [MaxApplicationLayouts]navigationPose {
	return m.current
}

func (m *navigationMotion) update(dt time.Duration) {
	if dt <= 0 || !m.moving() {
		return
	}
	// Compare before adding so even a very large elapsed duration cannot wrap.
	if dt >= navigationMotionDuration-m.elapsed {
		m.current, m.elapsed = m.target, navigationMotionDuration
		return
	}
	m.elapsed += dt
	t := float64(m.elapsed) / float64(navigationMotionDuration)
	// Quintic smootherstep has zero velocity and acceleration at both ends.
	// Sampling from absolute time makes the path independent of frame cadence.
	ease := float32(t * t * t * (t*(6*t-15) + 10))
	for i := range m.current {
		a, b := m.start[i].bounds, m.target[i].bounds
		m.current[i] = navigationPose{
			bounds: box{
				x: a.x + (b.x-a.x)*ease,
				y: a.y + (b.y-a.y)*ease,
				w: a.w + (b.w-a.w)*ease,
				h: a.h + (b.h-a.h)*ease,
			},
			visible: m.start[i].visible || m.target[i].visible,
		}
	}
}
