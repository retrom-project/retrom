import type { NextConfig } from "next";
import { unrestrictedDevOrigins } from "./lib/dev-origin";

const backend = process.env.NEXT_BACKEND_ORIGIN ?? "http://127.0.0.1:8080";

export const backendProxyLimits = {
  bodyBytes: 75 * 1024 * 1024,
  timeoutMs: 150_000
} as const;

const nextConfig: NextConfig = {
  allowedDevOrigins: unrestrictedDevOrigins(),
  distDir: process.env.NEXT_DIST_DIR ?? ".next",
  experimental: {
    proxyClientMaxBodySize: backendProxyLimits.bodyBytes,
    proxyTimeout: backendProxyLimits.timeoutMs
  },
  output: "standalone",
  poweredByHeader: false,
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
