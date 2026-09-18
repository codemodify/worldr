#version 450
layout(location=0) out vec4 outColor;
void main() {
    // An application is an opaque depth occluder, never a glow source.
    outColor = vec4(0.0);
}
