#version 450
#extension GL_GOOGLE_include_directive : require
#include "color.glsl"
layout(location=0) in vec3 inPosition;
layout(location=2) in vec4 inColor;
#include "draw.glsl"
layout(location=0) out vec4 emission;
void main() {
    gl_Position = draw.projection * (draw.model * vec4(inPosition,1.0));
    emission = vec4(linearColor ? decodeSRGB(draw.glow.rgb) : draw.glow.rgb,inColor.a*draw.color.a);
}
