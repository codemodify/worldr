#version 450
#extension GL_GOOGLE_include_directive : require
#include "color.glsl"
layout(constant_id=1) const bool outputSRGB = false;
layout(set=0,binding=0) uniform sampler2D content;
layout(push_constant) uniform OutputTransform {
    vec4 grade; // exposure stops, saturation offset, contrast offset, tone-map kind
    vec4 bloom; // strength, threshold, radius pixels (zero selects four), reserved
} outputTransform;
layout(location=0) in vec2 uv;
layout(location=0) out vec4 outColor;

vec3 straightSample(vec2 coordinate) {
    if (any(lessThan(coordinate,vec2(0.0))) || any(greaterThan(coordinate,vec2(1.0)))) return vec3(0.0);
    vec4 pixel=texture(content,coordinate);
    return pixel.a>0.0 ? pixel.rgb/pixel.a : vec3(0.0);
}
vec3 highlight(vec2 coordinate) {
    vec3 sampleColor=straightSample(coordinate);
    float luminance=dot(sampleColor,vec3(0.2126,0.7152,0.0722));
    float retained=max(luminance-outputTransform.bloom.y,0.0)/max(luminance,1e-6);
    return sampleColor*retained;
}
vec3 boundedBloom() {
    float radius=outputTransform.bloom.z>0.0 ? outputTransform.bloom.z : 4.0;
    vec2 step=radius/vec2(textureSize(content,0));
    // A fixed 13-tap disc keeps cost and temporal behavior independent of
    // brightness. Bilinear sampling supplies the soft subpixel footprint.
    const vec2 offsets[12]=vec2[12](
        vec2(1,0),vec2(-1,0),vec2(0,1),vec2(0,-1),
        vec2(.7071,.7071),vec2(-.7071,.7071),vec2(.7071,-.7071),vec2(-.7071,-.7071),
        vec2(.45,0),vec2(-.45,0),vec2(0,.45),vec2(0,-.45));
    vec3 sum=highlight(uv)*.16;
    for(int i=0;i<8;i++)sum+=highlight(uv+offsets[i]*step)*.06;
    for(int i=8;i<12;i++)sum+=highlight(uv+offsets[i]*step)*.09;
    return sum;
}
void main() {
    vec4 pixel = texture(content, uv); // Floating-point linear resolved target.
    float alpha = clamp(pixel.a,0.0,1.0);
    vec3 linear = alpha > 0.0 ? pixel.rgb / alpha : vec3(0.0);
    if (outputTransform.bloom.x > 0.0) linear += boundedBloom()*outputTransform.bloom.x;
    if (outputTransform.grade.x != 0.0) linear *= exp2(outputTransform.grade.x);
    if (outputTransform.grade.w > 0.5) {
        // Narkowicz' compact ACES fit provides a deterministic SDR shoulder;
        // it is a look transform, not an ACES color-management implementation.
        vec3 x=max(linear,vec3(0.0));
        linear=clamp((x*(2.51*x+.03))/(x*(2.43*x+.59)+.14),0.0,1.0);
    }
    if (outputTransform.grade.y != 0.0) {
        float luminance=dot(linear,vec3(0.2126,0.7152,0.0722));
        linear=max(mix(vec3(luminance),linear,1.0+outputTransform.grade.y),vec3(0.0));
    }
    if (outputTransform.grade.z != 0.0) {
        linear=max((linear-vec3(.18))*(1.0+outputTransform.grade.z)+vec3(.18),vec3(0.0));
    }
    // Return ordinary premultiplied sRGB bytes, including transparent captures.
    // Encoding an already-associated linear value would otherwise produce
    // RGB > alpha and invalid Go image.RGBA / native surface pixels.
    vec3 encoded = alpha > 0.0 ? encodeSRGB(linear) * alpha : vec3(0.0);
    outColor = vec4(outputSRGB ? decodeSRGB(encoded) : encoded, alpha);
}
