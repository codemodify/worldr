#version 450
#extension GL_GOOGLE_include_directive : require
#include "color.glsl"
layout(set=1,binding=0) uniform sampler2D content;
layout(location=0) in vec2 uv;
layout(location=0) out vec4 outColor;
void main() {
    // Screen overlays use premultiplied RGBA; opaque scene surfaces keep
    // their separate shader and depth contract.
    outColor = sampleContent(content, uv, true);
}
