import type { NextConfig } from "next";
import { localDevOrigins } from "./lib/dev-origin";

const backend = process.env.NEXT_BACKEND_ORIGIN ?? "http://127.0.0.1:8080";

export const backendProxyLimits = {
  bodyBytes: 283_115_520,
  timeoutMs: 300_000
} as const;

const nextConfig: NextConfig = {
  allowedDevOrigins: localDevOrigins(),
  distDir: process.env.NEXT_DIST_DIR ?? ".next",
  experimental: {
    proxyClientMaxBodySize: backendProxyLimits.bodyBytes,
    proxyTimeout: backendProxyLimits.timeoutMs
  },
  output: "standalone",
  poweredByHeader: false,
  transpilePackages: ["@xxxsen/retrom-runtime"],
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${backend}/api/:path*` },
      { source: "/health/:path*", destination: `${backend}/health/:path*` },
      { source: "/content/:path*", destination: `${backend}/content/:path*` },
      { source: "/runtime/:path*", destination: `${backend}/runtime/:path*` }
    ];
  }
};

export default nextConfig;
