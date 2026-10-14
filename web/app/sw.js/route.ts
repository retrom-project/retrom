import { readFileSync } from "node:fs";
import { join } from "node:path";

export const dynamic = "force-static";

export function GET() {
  const source = readFileSync(join(process.cwd(), "features/pwa/service-worker.js"), "utf8");
  const configuration = `const APP_VERSION = ${JSON.stringify(process.env.NEXT_PUBLIC_PWA_REVISION)};\nconst CACHE_STATIC = ${process.env.NODE_ENV === "production"};\n`;
  return new Response(configuration + source, {
    headers: {
      "Content-Type": "text/javascript; charset=utf-8",
      "Cache-Control": "no-cache, no-store, must-revalidate",
      "Service-Worker-Allowed": "/",
      "X-Content-Type-Options": "nosniff",
    },
  });
}
