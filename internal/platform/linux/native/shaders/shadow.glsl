layout(set=0,binding=1) uniform sampler2D shadowDepth;
float shadowVisibility(vec3 world) {
    if (draw.shadowParams.x <= 0.0 || draw.shadowParams.z < 0.5) return 1.0;
    vec4 clip = draw.shadowProjection * vec4(world,1.0);
    if (clip.w <= 0.0) return 1.0;
    vec3 coordinate = clip.xyz / clip.w;
    coordinate.xy = coordinate.xy * 0.5 + 0.5;
    if (any(lessThan(coordinate,vec3(0.0))) || any(greaterThan(coordinate,vec3(1.0)))) return 1.0;
    ivec2 size = textureSize(shadowDepth,0);
    ivec2 pixel = ivec2(coordinate.xy * vec2(size));
    float lit = 0.0;
    for (int y=-1;y<=1;y++) for (int x=-1;x<=1;x++) {
        ivec2 at = pixel + ivec2(x,y);
        // Border taps outside the authored light volume are unshadowed.
        if (any(lessThan(at,ivec2(0))) || any(greaterThanEqual(at,size))) lit += 1.0;
        else lit += coordinate.z - draw.shadowParams.y <= texelFetch(shadowDepth,at,0).r ? 1.0 : 0.0;
    }
    return 1.0 - draw.shadowParams.x * (1.0-lit/9.0);
}
