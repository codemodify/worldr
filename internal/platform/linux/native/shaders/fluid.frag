#version 450
// Authored here for Worldr: an analytic rounded-panel field, independently
// shaded over a procedural backdrop. Text is a later ordered overlay.
layout(constant_id=0) const uint linearColor=0;
struct Surface { vec4 bounds; vec4 shape; vec4 tint; };
layout(std140,set=0,binding=0) uniform Fluid {
    vec4 bounds;
    vec4 style;       // blend pixels, rim, refraction, frost
    vec4 finish;      // glow, opacity, time, reserved
    vec4 pointer;     // x, y, surface count, active
    vec4 background[3];
    Surface surfaces[16];
} field;
layout(location=0) out vec4 outColor;

float roundedBox(vec2 p, Surface s,out vec2 gradient) {
    vec2 delta=p-s.bounds.xy-s.bounds.zw*.5;
    vec2 q=abs(delta)-(s.bounds.zw*.5-s.shape.x);
    vec2 outer=max(q,vec2(0));
    float distance=length(outer);
    gradient=distance>.00001?sign(delta)*outer/distance:
        q.x>q.y?vec2(sign(delta.x),0):vec2(0,sign(delta.y));
    return distance+min(max(q.x,q.y),0)-s.shape.x;
}

// Match sdk/fluid/v1 exactly: only enabled surfaces form the smooth union;
// disabled surfaces enter a second hard-union pass and can never seed a bridge.
float distanceField(vec2 p,out vec4 tint,out vec2 gradient) {
    float d=1e10;
    tint=vec4(0);
    gradient=vec2(0);
    int count=int(field.pointer.z);
    for(int i=0;i<count;i++) {
        Surface s=field.surfaces[i];
        if(s.shape.y<.5)continue;
        vec2 nextGradient;
        float b=roundedBox(p,s,nextGradient),k=field.style.x;
        float h=k>0?max(k-abs(d-b),0)/k:0;
        float weight=k>0?clamp(.5+.5*(d-b)/k,0,1):float(b<d);
        tint=mix(tint,s.tint,weight);
        gradient=mix(gradient,nextGradient,weight);
        d=min(d,b)-k*h*h*.25;
    }
    for(int i=0;i<count;i++) {
        Surface s=field.surfaces[i];
        if(s.shape.y>=.5)continue;
        vec2 nextGradient;
        float b=roundedBox(p,s,nextGradient);
        if(b<d){d=b;tint=s.tint;gradient=nextGradient;}
    }
    return d;
}

vec3 backdrop(vec2 pixel,float softened) {
    vec2 uv=(pixel-field.bounds.xy)/field.bounds.zw;
    float t=field.finish.z;
    vec2 q=(uv-vec2(.5))*vec2(field.bounds.z/field.bounds.w,1);
    float left=exp(-dot(q-vec2(-.38,.2),q-vec2(-.38,.2))*2.4);
    float right=exp(-dot(q-vec2(.52,-.3),q-vec2(.52,-.3))*3.6);
    vec3 color=mix(field.background[0].rgb,field.background[1].rgb,left*.72);
    color=mix(color,field.background[2].rgb,right*.53);
    // A bounded domain warp makes the strata fold back into broad concave
    // pockets. Analytic contours stay attached to the background as glass
    // bends them, making refraction legible without an uploaded wallpaper.
    vec2 w=q+.13*vec2(sin(q.y*4.8+1.3*cos(q.x*3.2)+t*.029),
                        cos(q.x*4.1-1.2*sin(q.y*3.6)-t*.023));
    float fold=w.x+.48*w.y+.27*sin(w.x*3.7+w.y*4.4)+.19*cos(w.x*6.2-w.y*2.5);
    float phase=fold*24+.3*sin(w.y*14+w.x*6+t*.018);
    float ridge=exp(-abs(sin(phase))/mix(.07,.24,softened));
    float strata=.5+.5*sin(phase-.36);
    vec3 mineral=mix(field.background[1].rgb,field.background[2].rgb,
                     .5+.5*sin(fold*3.2+.8));
    float presence=clamp(dot(field.background[0].rgb+field.background[1].rgb+field.background[2].rgb,vec3(1)),0,1);
    color*=.78+.22*strata;
    color+=(mineral*.46+vec3(.035,.068,.068)*presence)*ridge;
    // One wider traveling caustic supplies a quiet large-scale highlight.
    float ribbon=q.y+.16*sin(q.x*4.3+t*.055)+.29*sin(q.x*1.8-t*.033);
    color+=mineral*.12*exp(-abs(ribbon+.12)/mix(.009,.028,softened));
    return max(color,vec3(0));
}

vec3 decodeSRGB(vec3 c) {
    return mix(c/12.92,pow((c+.055)/1.055,vec3(2.4)),greaterThan(c,vec3(.04045)));
}

void main() {
    vec2 p=gl_FragCoord.xy;
    // Integer scissors conservatively enclose fractional authored bounds.
    if(any(lessThan(p,field.bounds.xy)) || any(greaterThanEqual(p,field.bounds.xy+field.bounds.zw)))discard;
    vec3 base=backdrop(p,0);
    vec4 tint;
    vec2 gradient;
    float d=distanceField(p,tint,gradient);
    vec2 normal=gradient/max(length(gradient),.001);
    // Analytic derivatives keep shading identical across odd physical-output
    // boundaries, where hardware derivative quads have different origins.
    float aa=max(.7,(abs(gradient.x)+abs(gradient.y))*.65);
    float inside=1-smoothstep(-aa,aa,d);
    float edge=exp(-abs(d)/9);
    float time=field.finish.z;
    float hover=field.pointer.w*exp(-dot(p-field.pointer.xy,p-field.pointer.xy)/26000);
    // The center of a pane is optically flat. Taper the lens before the SDF's
    // interior medial axis, where a nearest-edge normal can change abruptly.
    vec2 bend=normal*field.style.z*28*exp(-abs(d)/14);
    vec3 transmitted=backdrop(p-bend,field.style.w);
    vec3 glass=mix(transmitted,tint.rgb,tint.a*.42);
    // Frost softens procedural details, while restrained opposing washes give
    // a broad panel a visible glass body without clouding the later typography.
    glass+=vec3(.042,.067,.073)*field.style.w;
    glass+=vec3(.023,.045,.044)*max(0,-normal.y)*edge;
    vec2 local=p-field.bounds.xy;
    float phase=local.x*.0017+local.y*.0011+time*.035;
    vec3 iridescent=.5+.5*cos(vec3(0,2.1,4.2)+phase*6.28318+normal.x*1.4-normal.y*.7);
    iridescent=mix(vec3(.26,.88,.85),iridescent,.55);
    float rim=exp(-abs(d+.25)*1.13);
    float inner=exp(-abs(d+2.3)*1.5)*.18;
    float gleam=pow(max(0,dot(normal,normalize(vec2(-.6,-.8)))),3);
    vec3 result=mix(base,glass,inside*field.finish.y);
    result+=iridescent*(rim+inner)*field.style.y*(.48+.45*gleam+.2*hover)*field.finish.y;
    result+=iridescent*exp(-abs(d)/11)*field.finish.x*.15*field.finish.y;
    result+=vec3(.48,.66,.64)*rim*field.style.y*gleam*.2*field.finish.y;
    result=clamp(result,vec3(0),vec3(1));
    if(linearColor!=0)result=decodeSRGB(result);
    outColor=vec4(result,1);
}
