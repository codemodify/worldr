#version 450
layout(location=2) in vec4 color;
void main() { if (color.a <= .001) discard; }
