layout(set=0,binding=0) uniform Draw {
    mat4 projection;
    mat4 model;
    vec4 eye;
    vec4 light;
    vec4 color;
    vec4 wire;
    vec4 params; // wire width, unlit, normalized native-effect phase, reserved
    vec4 material;
    vec4 rim;
    vec4 glow;
    vec4 optical; // transmission, refraction, refraction blur, hologram strength
    vec4 pointPosition[4]; // xyz, radius
    vec4 pointColor[4]; // linear/sRGB RGB, intensity
    mat4 shadowProjection;
    vec4 shadowParams; // strength, receiver bias, receive enabled, reserved
} draw;
