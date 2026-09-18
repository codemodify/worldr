#version 450
#extension GL_GOOGLE_include_directive : require
#include "draw.glsl"
layout(location=0) in vec3 inPosition;
void main() { gl_Position = draw.shadowProjection * draw.model * vec4(inPosition,1.0); }
