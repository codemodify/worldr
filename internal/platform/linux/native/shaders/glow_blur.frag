#version 450
layout(set=0,binding=0) uniform sampler2D source;
layout(push_constant) uniform Blur { vec2 step; } blur;
layout(location=0) in vec2 uv;
layout(location=0) out vec4 outColor;
vec3 sampleSeed(vec2 at) {
    // Out-of-target samples are transparent, never clamped bright borders.
    if (any(lessThan(at,vec2(0.0))) || any(greaterThan(at,vec2(1.0)))) return vec3(0.0);
    return texture(source,at).rgb;
}
void main() {
    const float weights[5]=float[5](0.2270270270,0.1945945946,0.1216216216,0.0540540541,0.0162162162);
    vec3 light=sampleSeed(uv)*weights[0];
    for (int i=1;i<=4;i++) {
        vec2 offset=blur.step*float(i);
        light+=(sampleSeed(uv-offset)+sampleSeed(uv+offset))*weights[i];
    }
    outColor=vec4(light,0.0);
}
