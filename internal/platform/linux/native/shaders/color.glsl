// Authored colors and RGBA8 app pixels are sRGB. Alpha is linear coverage.
// Floating-point linear targets keep lighting, blending and MSAA resolves in
// linear light. The output pass applies sRGB encoding once, after resolving.
layout(constant_id=0) const bool linearColor = false;
vec3 decodeSRGB(vec3 value) {
    value = max(value, vec3(0.0));
    return mix(value / 12.92, pow((value + 0.055) / 1.055, vec3(2.4)), greaterThan(value, vec3(0.04045)));
}
vec3 encodeSRGB(vec3 value) {
    value = max(value, vec3(0.0));
    return mix(value * 12.92, 1.055 * pow(value, vec3(1.0/2.4)) - 0.055, greaterThan(value, vec3(0.0031308)));
}
vec4 linearTexel(sampler2D source, ivec2 at, bool premultiplied) {
    ivec2 size = textureSize(source, 0);
    vec4 pixel = texelFetch(source, clamp(at, ivec2(0), size-1), 0);
    if (premultiplied) pixel.rgb = pixel.a > 0.0 ? decodeSRGB(pixel.rgb / pixel.a) * pixel.a : vec3(0.0);
    else pixel.rgb = decodeSRGB(pixel.rgb);
    return pixel;
}
vec4 sampleContent(sampler2D source, vec2 coordinate, bool premultiplied) {
    if (!linearColor) return texture(source, coordinate);
    // Decode before filtering. Decoding one already-interpolated UNORM sample
    // gives the wrong midtone; sRGB-encoded premultiplied images additionally
    // need unassociation/reassociation around the transfer function.
    vec2 at = coordinate * vec2(textureSize(source, 0)) - 0.5;
    ivec2 base = ivec2(floor(at));
    vec2 f = fract(at);
    return mix(mix(linearTexel(source, base, premultiplied), linearTexel(source, base+ivec2(1,0), premultiplied), f.x),
               mix(linearTexel(source, base+ivec2(0,1), premultiplied), linearTexel(source, base+ivec2(1,1), premultiplied), f.x), f.y);
}
