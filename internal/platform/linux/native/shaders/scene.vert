#version 450
#extension GL_GOOGLE_include_directive : require
#include "color.glsl"
layout(location = 0) in vec3 inPosition;
layout(location = 1) in vec2 inUV;
layout(location = 2) in vec4 inColor;
layout(push_constant) uniform Frame { vec2 extent; } frame;
layout(location = 0) out vec2 uv;
layout(location = 1) out vec4 color;
void main() {
    gl_Position = vec4(2.0 * inPosition.xy / frame.extent - 1.0, inPosition.z, 1.0);
    uv = inUV;
    color = vec4(linearColor ? decodeSRGB(inColor.rgb) : inColor.rgb, inColor.a);
}
