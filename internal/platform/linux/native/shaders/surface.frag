#version 450
#extension GL_GOOGLE_include_directive : require
#include "color.glsl"
#include "draw.glsl"
#include "shadow.glsl"
layout(set=1,binding=0) uniform sampler2D content;
layout(location=0) in vec2 uv;
layout(location=1) in vec4 tint;
layout(location=2) in vec3 worldPosition;
layout(location=0) out vec4 outColor;
void main() {
    // Surfaces currently have an opaque coverage contract for both depth and picking.
    outColor = vec4(sampleContent(content, uv, false).rgb * tint.rgb * shadowVisibility(worldPosition), 1.0);
}
