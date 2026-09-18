#version 450
layout(location=0) in vec4 emission;
layout(location=0) out vec4 outColor;
void main() {
    if (emission.a <= 0.001) discard;
    outColor = vec4(emission.rgb*clamp(emission.a,0.0,1.0),0.0);
}
