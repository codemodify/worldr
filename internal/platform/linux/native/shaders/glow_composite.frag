#version 450
layout(set=0,binding=0) uniform sampler2D halo;
layout(location=0) in vec2 uv;
layout(location=0) out vec4 outColor;
void main() {
    outColor=vec4(texture(halo,uv).rgb,0.0);
}
