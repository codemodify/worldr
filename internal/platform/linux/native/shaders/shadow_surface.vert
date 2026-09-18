#version 450
#extension GL_GOOGLE_include_directive : require
#include "draw.glsl"
const vec2 corners[6] = vec2[6](vec2(0,0),vec2(0,1),vec2(1,1),vec2(0,0),vec2(1,1),vec2(1,0));
void main() { vec2 uv=corners[gl_VertexIndex];gl_Position=draw.shadowProjection*draw.model*vec4(uv.x-.5,.5-uv.y,0,1); }
