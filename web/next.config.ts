import type { NextConfig } from "next";
import { allowedDevOriginsFromPublicOrigin } from "./lib/dev-origin";

const backend = process.env.NEXT_BACKEND_ORIGIN ?? "http://127.0.0.1:8080";

const nextConfig: NextConfig = {
  allowedDevOrigins: allowedDevOriginsFromPublicOrigin(process.env.RETROM_PUBLIC_ORIGIN),
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
