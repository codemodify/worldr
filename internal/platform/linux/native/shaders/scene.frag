#version 450
layout(set = 0, binding = 0) uniform sampler2D coverageAtlas;
layout(location = 0) in vec2 uv;
layout(location = 1) in vec4 color;
layout(location = 0) out vec4 outColor;
void main() {
    float alpha = clamp(color.a * texture(coverageAtlas, uv).r,0.0,1.0);
    // Transparent atlas pixels must not occlude geometry behind the glyph.
    if (alpha <= 0.001) discard;
    outColor = vec4(color.rgb, alpha);
}
