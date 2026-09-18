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
#ifdef DEPTH_PEEL
#include "peel.glsl"
#endif
void main() {
#ifdef DEPTH_PEEL
    peelFragment();
#endif
    vec4 pixel = sampleContent(content, uv, true);
    if (pixel.a * tint.a <= .001) discard;
    // Texture RGB is premultiplied. Tint RGB is straight, tint alpha attenuates
    // both coverage and emitted RGB exactly once; no depth write is performed.
    outColor = vec4(pixel.rgb * tint.rgb * tint.a * shadowVisibility(worldPosition), pixel.a * tint.a);
}
