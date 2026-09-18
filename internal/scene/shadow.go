package scene

import (
	"fmt"
	"github.com/codemodify/worldr/internal/render"
)

// DirectionalShadow builds a stable orthographic light volume centered on the
// supplied world point. Span is its width/height; depth is its extent along the
// direction toward the light. Choose the smallest volume containing casters and
// receivers. The fixed 1024-pixel map is independent of viewport resolution.
func DirectionalShadow(center, towardLight Vec3, span, depth float32) (render.Shadow, error) {
	for _, v := range []float32{center.X, center.Y, center.Z, towardLight.X, towardLight.Y, towardLight.Z, span, depth} {
		if !finite(v) {
			return render.Shadow{}, fmt.Errorf("non-finite shadow volume")
		}
	}
	if span <= 0 || depth <= 0 || towardLight.Length() < 1e-6 {
		return render.Shadow{}, fmt.Errorf("shadow volume needs positive extents and a light direction")
	}
	largest := max(abs(towardLight.X), max(abs(towardLight.Y), abs(towardLight.Z)))
	light := towardLight.Mul(1 / largest).Normalize()
	up := Vec3{0, 1, 0}
	if abs(light.Dot(up)) > .98 {
		up = Vec3{0, 0, 1}
	}
	right := up.Cross(light).Normalize()
	up = light.Cross(right).Normalize()
	projection := Identity()
	projection[0], projection[4], projection[8], projection[12] = 2*right.X/span, 2*right.Y/span, 2*right.Z/span, -2*right.Dot(center)/span
	projection[1], projection[5], projection[9], projection[13] = -2*up.X/span, -2*up.Y/span, -2*up.Z/span, 2*up.Dot(center)/span
	projection[2], projection[6], projection[10], projection[14] = -light.X/depth, -light.Y/depth, -light.Z/depth, .5+light.Dot(center)/depth
	for _, v := range projection {
		if !finite(v) {
			return render.Shadow{}, fmt.Errorf("shadow projection exceeds finite precision")
		}
	}
	return render.Shadow{Projection: [16]float32(projection), Strength: .8, Bias: .0015}, nil
}
