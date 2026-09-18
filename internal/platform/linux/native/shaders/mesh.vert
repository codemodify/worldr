#version 450
#extension GL_GOOGLE_include_directive : require
#include "color.glsl"
layout(location=0) in vec3 inPosition;
layout(location=1) in vec3 inNormal;
layout(location=2) in vec4 inColor;
layout(location=3) in vec3 inBarycentric;
#include "draw.glsl"
layout(location=0) out vec3 worldPosition;
layout(location=1) out vec3 worldNormal;
layout(location=2) out vec4 color;
layout(location=3) noperspective out vec3 barycentric;
void main() {
    vec4 world = draw.model * vec4(inPosition,1.0);
    gl_Position = draw.projection * world;
    worldPosition = world.xyz;
    worldNormal = transpose(inverse(mat3(draw.model))) * inNormal;
    color = linearColor ? vec4(decodeSRGB(inColor.rgb) * decodeSRGB(draw.color.rgb), inColor.a * draw.color.a) : inColor * draw.color;
    barycentric = inBarycentric;
}
