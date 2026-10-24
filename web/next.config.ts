import type { NextConfig } from "next";
import { localDevOrigins } from "./lib/dev-origin";
import { pwaRevision } from "./lib/pwa-build";

const backend = process.env.NEXT_BACKEND_ORIGIN ?? "http://127.0.0.1:8080";

export const backendProxyLimits = {
  bodyBytes: 283_115_520,
  // External rewrites share this Next.js limit. Go keeps ordinary requests at
  // ten minutes and content replacement at ninety, with time to return a result.
  timeoutMs: 91 * 60_000
} as const;

const nextConfig: NextConfig = {
  env: { NEXT_PUBLIC_PWA_REVISION: pwaRevision(import.meta.dirname) },
  devIndicators: process.env.NEXT_WEB_E2E === "true" ? false : { position: "bottom-right" },
  allowedDevOrigins: localDevOrigins(),
  distDir: process.env.NEXT_DIST_DIR ?? ".next",
  experimental: {
    proxyClientMaxBodySize: backendProxyLimits.bodyBytes,
    proxyTimeout: backendProxyLimits.timeoutMs
  },
  output: "standalone",
  poweredByHeader: false,
  async headers() {
    return [{ source: "/sw.js", headers: [{ key: "Cache-Control", value: "no-cache, no-store, must-revalidate" }] }];
  },
  redirects() {
    return [{ source: "/favicon.ico", destination: "/icon.svg", permanent: true }];
  },
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
