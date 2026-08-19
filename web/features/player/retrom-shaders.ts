type TextShaderResource = {
  name: string;
  type: "text";
  value: string;
};

type RetromShader = {
  shader: { type: "text"; value: string };
  resources: TextShaderResource[];
};

const compatibilityHeader = `
#if defined(VERTEX)

#if __VERSION__ >= 130
#define COMPAT_VARYING out
#define COMPAT_ATTRIBUTE in
#else
#define COMPAT_VARYING varying
#define COMPAT_ATTRIBUTE attribute
#endif

#ifdef GL_ES
#define COMPAT_PRECISION mediump
#else
#define COMPAT_PRECISION
#endif

COMPAT_ATTRIBUTE vec4 VertexCoord;
COMPAT_ATTRIBUTE vec4 TexCoord;
COMPAT_VARYING vec4 TEX0;
uniform mat4 MVPMatrix;

void main()
{
  gl_Position = MVPMatrix * VertexCoord;
  TEX0 = TexCoord;
}

#elif defined(FRAGMENT)

#ifdef GL_ES
#ifdef GL_FRAGMENT_PRECISION_HIGH
precision highp float;
#else
precision mediump float;
#endif
#define COMPAT_PRECISION mediump
#else
#define COMPAT_PRECISION
#endif

#if __VERSION__ >= 130
#define COMPAT_VARYING in
#define COMPAT_TEXTURE texture
out COMPAT_PRECISION vec4 FragColor;
#else
#define COMPAT_VARYING varying
#define COMPAT_TEXTURE texture2D
#define FragColor gl_FragColor
#endif

uniform COMPAT_PRECISION vec2 OutputSize;
uniform COMPAT_PRECISION vec2 TextureSize;
uniform COMPAT_PRECISION vec2 InputSize;
uniform sampler2D Texture;
COMPAT_VARYING vec4 TEX0;
`;

const sharpBilinearSource = `${compatibilityHeader}
void main()
{
  vec2 sourceSize = max(InputSize, vec2(1.0));
  vec2 textureDimensions = max(TextureSize, vec2(1.0));
  vec2 outputScale = max(floor(OutputSize / sourceSize), vec2(1.0));
  vec2 texel = TEX0.xy * textureDimensions;
  vec2 center = floor(texel) + vec2(0.5);
  vec2 centerDistance = texel - center;
  vec2 linearRegion = vec2(0.5) - vec2(0.5) / outputScale;
  vec2 offset = (centerDistance - clamp(centerDistance, -linearRegion, linearRegion)) * outputScale;
  vec2 coordinate = (center + offset) / textureDimensions;
  FragColor = COMPAT_TEXTURE(Texture, coordinate);
}
#endif
`;

const adaptiveSharpenSource = `${compatibilityHeader}
void main()
{
  vec2 texel = vec2(1.0) / max(TextureSize, vec2(1.0));
  vec4 center = COMPAT_TEXTURE(Texture, TEX0.xy);
  vec3 north = COMPAT_TEXTURE(Texture, TEX0.xy + vec2(0.0, -texel.y)).rgb;
  vec3 south = COMPAT_TEXTURE(Texture, TEX0.xy + vec2(0.0, texel.y)).rgb;
  vec3 west = COMPAT_TEXTURE(Texture, TEX0.xy + vec2(-texel.x, 0.0)).rgb;
  vec3 east = COMPAT_TEXTURE(Texture, TEX0.xy + vec2(texel.x, 0.0)).rgb;
  vec3 detail = center.rgb * 4.0 - north - south - west - east;
  float contrast = max(max(abs(detail.r), abs(detail.g)), abs(detail.b));
  float strength = mix(0.24, 0.12, smoothstep(0.20, 0.75, contrast));
  FragColor = vec4(clamp(center.rgb + detail * strength, 0.0, 1.0), center.a);
}
#endif
`;

function shader(preset: string, resourceName: string, source: string): RetromShader {
  return {
    shader: { type: "text", value: preset },
    resources: [{ name: resourceName, type: "text", value: source }],
  };
}

// These two small shaders are authored by Retrom and shipped with the web
// application. They avoid adding a second, unpinned third-party shader payload.
export const retromShaders: Record<string, RetromShader> = {
  "retrom-sharp-bilinear": shader(
    'shaders = 1\nshader0 = "retrom-sharp-bilinear.glsl"\nfilter_linear0 = true\n',
    "retrom-sharp-bilinear.glsl",
    sharpBilinearSource,
  ),
  "retrom-adaptive-sharpen": shader(
    'shaders = 1\nshader0 = "retrom-adaptive-sharpen.glsl"\nfilter_linear0 = true\n',
    "retrom-adaptive-sharpen.glsl",
    adaptiveSharpenSource,
  ),
};
