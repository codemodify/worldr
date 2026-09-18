#version 450
#extension GL_GOOGLE_include_directive : require
#include "color.glsl"
#include "draw.glsl"
layout(location=0) out vec2 uv;
layout(location=1) out vec4 tint;
layout(location=2) out vec3 worldPosition;
const vec2 corners[6] = vec2[6](
    vec2(0,0), vec2(0,1), vec2(1,1),
    vec2(0,0), vec2(1,1), vec2(1,0));
void main() {
    vec2 corner = corners[gl_VertexIndex];
    uv = draw.material.xy + corner * draw.material.zw;
    vec4 world = draw.model * vec4(corner.x - 0.5, 0.5 - corner.y, 0, 1);
    gl_Position = draw.projection * world;
    worldPosition = world.xyz;
    tint = vec4(linearColor ? decodeSRGB(draw.color.rgb) : draw.color.rgb, draw.color.a);
}
