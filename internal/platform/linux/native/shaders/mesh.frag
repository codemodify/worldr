#version 450
#extension GL_GOOGLE_include_directive : require
#include "color.glsl"
#include "draw.glsl"
#include "shadow.glsl"
layout(location=0) in vec3 worldPosition;
layout(location=1) in vec3 worldNormal;
layout(location=2) in vec4 color;
layout(location=3) noperspective in vec3 barycentric;
layout(location=0) out vec4 outColor;
#ifdef DEPTH_PEEL
#include "peel.glsl"
#endif
void main() {
#ifdef DEPTH_PEEL
    peelFragment();
#endif
    vec3 normal = normalize(worldNormal);
    if (dot(normal, draw.eye.xyz-worldPosition) < 0.0) normal = -normal;
    float visibility = shadowVisibility(worldPosition);
    float keyLength2=dot(draw.light.xyz,draw.light.xyz);
    vec3 keyLight=keyLength2>1e-8 ? draw.light.xyz*inversesqrt(keyLength2) : vec3(0.0);
    float illumination = draw.params.y > 0.5 ? 1.0 : 0.28 + 0.72 * max(dot(normal,keyLight),0.0) * visibility;
    vec3 surface = color.rgb * illumination;
    // Keep the original diffuse/unlit path exact for existing applications.
    // This is bounded direct lighting, not a full physical material model.
    if (draw.params.y <= 0.5 && (draw.material.x > 0.0 || draw.material.w > 0.0 || draw.optical.x > 0.0 || draw.light.w > 0.5)) {
        vec3 toEye = draw.eye.xyz-worldPosition;
        float normalLength2 = dot(worldNormal,worldNormal);
        normal = normalLength2 > 1e-8 ? worldNormal*inversesqrt(normalLength2) : vec3(0.0,0.0,1.0);
        if (dot(normal,toEye) < 0.0) normal = -normal;
        float eyeLength2 = dot(toEye,toEye);
        vec3 view = eyeLength2 > 1e-8 ? toEye*inversesqrt(eyeLength2) : normal;
        vec3 light = keyLight;
        float normalLight = max(dot(normal,light),0.0);
        float normalView = clamp(dot(normal,view),0.0,1.0);
        float body=1.0-.9*draw.optical.x;
        surface = color.rgb * (0.28 + 0.72*normalLight*visibility*(1.0-0.35*draw.material.z*draw.material.x))*body;
        if (draw.material.x > 0.0 && normalLight > 0.0) {
            vec3 halfway = light+view;
            float halfwayLength2 = dot(halfway,halfway);
            if (halfwayLength2 > 1e-8) {
                halfway *= inversesqrt(halfwayLength2);
                float roughness = max(draw.material.y,0.08);
                float exponent = max(2.0,2.0/(roughness*roughness)-2.0);
                float highlight = pow(max(dot(normal,halfway),0.0),exponent)*normalLight*visibility;
                vec3 reflection = mix(vec3(0.08),clamp(color.rgb,0.0,1.0),draw.material.z);
                float grazing = pow(1.0-clamp(dot(view,halfway),0.0,1.0),5.0);
                reflection += (vec3(1.0)-reflection)*grazing;
                surface += reflection*highlight*draw.material.x;
            }
        }
        // Four fixed slots keep the uniform and shader cost bounded. Radius
        // attenuation reaches exactly zero, so moving lights never changes
        // geometry, depth, or picking bounds.
        int pointCount=clamp(int(draw.light.w+.5),0,4);
        for (int i=0;i<pointCount;i++) {
            vec3 toLight=draw.pointPosition[i].xyz-worldPosition;
            float distance2=dot(toLight,toLight);
            float radius=draw.pointPosition[i].w;
            if (distance2<=1e-8 || distance2>=radius*radius) continue;
            float distanceToLight=sqrt(distance2);
            vec3 pointDirection=toLight/distanceToLight;
            float falloff=1.0-distanceToLight/radius;
            float pointAmount=max(dot(normal,pointDirection),0.0)*falloff*falloff*draw.pointColor[i].w;
            if (pointAmount<=0.0) continue;
            vec3 pointColor=linearColor ? decodeSRGB(draw.pointColor[i].rgb) : draw.pointColor[i].rgb;
            surface += color.rgb*pointColor*pointAmount*body;
            if (draw.material.x>0.0) {
                vec3 halfway=pointDirection+view;
                float halfwayLength2=dot(halfway,halfway);
                if (halfwayLength2>1e-8) {
                    halfway*=inversesqrt(halfwayLength2);
                    float roughness=max(draw.material.y,.08);
                    float exponent=max(2.0,2.0/(roughness*roughness)-2.0);
                    float highlight=pow(max(dot(normal,halfway),0.0),exponent)*pointAmount;
                    vec3 reflection=mix(vec3(.08),clamp(color.rgb,0.0,1.0),draw.material.z);
                    float grazing=pow(1.0-clamp(dot(view,halfway),0.0,1.0),5.0);
                    reflection+=(vec3(1.0)-reflection)*grazing;
                    surface+=reflection*pointColor*highlight*draw.material.x;
                }
            }
        }
        // A subtle cool contour follows geometry and camera angle. It cannot
        // spill over neighboring surfaces or change the scene's depth order.
        float rim = pow(1.0-normalView,3.0)*(0.25+0.75*(1.0-normalLight));
        surface += (linearColor ? decodeSRGB(draw.rim.rgb) : draw.rim.rgb)*rim*draw.material.w;
#ifdef DEPTH_PEEL
        if(draw.optical.y>0.0) {
            // Project the smooth world normal as a direction (w=0), then bend
            // the opaque backdrop by at most 18 output pixels. Face-on glass
            // remains stable while curved or oblique glass produces the
            // strongest displacement. Refraction is mesh-only and opt-in.
            vec2 projected=(draw.projection*vec4(normal,0.0)).xy;
            float projectedLength=length(projected);
            vec2 direction=projectedLength>1e-6?projected/projectedLength:vec2(0.0);
            float bend=draw.optical.y*(3.0+15.0*(1.0-normalView));
            vec3 backdrop=refractedBackdrop(direction*bend,draw.optical.z);
            surface=mix(surface,backdrop,draw.optical.y);
        }
#endif
    }
#ifdef DEPTH_PEEL
    float hologramCoverage=1.0;
    if (draw.optical.w>0.0) {
        vec3 toEye=draw.eye.xyz-worldPosition;
        float eyeLength2=dot(toEye,toEye);
        vec3 view=eyeLength2>1e-8?toEye*inversesqrt(eyeLength2):normal;
        float normalLength2=dot(worldNormal,worldNormal);
        vec3 hologramNormal=normalLength2>1e-8?worldNormal*inversesqrt(normalLength2):vec3(0.0,0.0,1.0);
        if (dot(hologramNormal,view)<0.0) hologramNormal=-hologramNormal;
        float fresnel=pow(1.0-clamp(dot(hologramNormal,view),0.0,1.0),2.2);
        float phase=draw.params.z*6.28318530718;
        // Bands live in world space, so orbiting or resizing a window does not
        // make them crawl in screen coordinates. The phase is host-owned and
        // can be frozen independently of application playback.
        float broad=0.5+0.5*sin(worldPosition.y*52.0-phase);
        float band=smoothstep(0.72,0.96,broad);
        float fine=0.5+0.5*sin((worldPosition.y+worldPosition.x*0.07)*173.0+phase*2.0);
        float contact=opaqueContactBand();
        vec3 authored=draw.rim.rgb;
        vec3 tint=dot(authored,authored)>1e-6?(linearColor?decodeSRGB(authored):authored):max(color.rgb,vec3(0.01));
        float field=0.16+0.74*fresnel+0.46*band+0.12*fine+1.15*contact;
        vec3 projected=tint*field;
        surface=mix(surface,projected,draw.optical.w);
        float projectedCoverage=clamp(0.12+0.52*fresnel+0.28*band+0.32*contact,0.08,1.0);
        hologramCoverage=mix(1.0,projectedCoverage,draw.optical.w);
    }
#else
    const float hologramCoverage=1.0;
#endif
    if (draw.params.x > 0.0 && draw.wire.a > 0.0) {
        vec3 distancePixels = barycentric / max(fwidth(barycentric), vec3(1e-6));
        float distanceToEdge = min(distancePixels.x,min(distancePixels.y,distancePixels.z));
        float edge = 1.0-smoothstep(max(0.0,draw.params.x-0.5),draw.params.x+0.5,distanceToEdge);
        surface = mix(surface, linearColor ? decodeSRGB(draw.wire.rgb) : draw.wire.rgb, edge*draw.wire.a);
    }
    if (color.a <= 0.001) discard;
    outColor = vec4(surface,clamp(color.a*hologramCoverage,0.0,1.0));
#ifdef DEPTH_PEEL
    outColor.rgb *= outColor.a;
#endif
}
