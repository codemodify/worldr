layout(set=2,binding=0) uniform sampler2D opaqueDepth;
layout(set=2,binding=1) uniform sampler2D previousDepth;
layout(set=2,binding=2) uniform sampler2D opaqueBackdrop;
layout(push_constant) uniform Peel { int first; } peel;
void peelFragment() {
    ivec2 pixel=ivec2(gl_FragCoord.xy);
    float depth=gl_FragCoord.z;
    if (depth > texelFetch(opaqueDepth,pixel,0).r) discard;
    if (peel.first==0 && depth <= texelFetch(previousDepth,pixel,0).r) discard;
}

float opaqueContactBand() {
    ivec2 pixel=ivec2(gl_FragCoord.xy);
    float opaque=texelFetch(opaqueDepth,pixel,0).r;
    if (opaque>=0.999999) return 0.0;
    // Only fragments in front of the opaque scene survive peelFragment. A
    // narrow normalized-depth band turns their approach into a contact glow;
    // it never reveals a projection through an opaque object.
    float gap=max(opaque-gl_FragCoord.z,0.0);
    return 1.0-smoothstep(0.00035,0.006,gap);
}

vec3 refractedBackdrop(vec2 offsetPixels,float blurAmount) {
    vec2 extent=vec2(textureSize(opaqueBackdrop,0));
    vec2 uv=clamp((gl_FragCoord.xy+offsetPixels)/extent,vec2(.5)/extent,vec2(1.0)-vec2(.5)/extent);
    vec3 result=texture(opaqueBackdrop,uv).rgb;
    if(blurAmount<=0.0)return result;
    vec2 step=vec2(6.0*blurAmount)/extent;
    // A fixed five-tap cross gives frosted native glass a bounded cost. The
    // sampled image contains only the opaque scene; UI and legacy pixels are
    // never globally filtered or recolored.
    return result*.4+(texture(opaqueBackdrop,uv+vec2(step.x,0)).rgb+
        texture(opaqueBackdrop,uv-vec2(step.x,0)).rgb+
        texture(opaqueBackdrop,uv+vec2(0,step.y)).rgb+
        texture(opaqueBackdrop,uv-vec2(0,step.y)).rgb)*.15;
}
