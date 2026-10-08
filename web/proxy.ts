import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";
export function proxy(request: NextRequest) {
  const response = NextResponse.next();
  response.headers.set("X-Content-Type-Options", "nosniff");
  response.headers.set("Referrer-Policy", "same-origin");
  response.headers.set("Cross-Origin-Opener-Policy", "same-origin");
  response.headers.set("Cross-Origin-Embedder-Policy", "require-corp");
  response.headers.set("Cross-Origin-Resource-Policy", "same-origin");
  if (!request.nextUrl.pathname.startsWith("/__retrom/runtime-isolation/")) {
    response.headers.set(
      "Content-Security-Policy",
      "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' blob:; style-src 'self' 'unsafe-inline'; img-src 'self' blob: data:; media-src 'self' blob:; connect-src 'self' blob: ws: wss:; worker-src 'self' blob:; frame-src 'self' http://*.localhost:3000 https:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'",
    );
  }
  return response;
}
export const config = {
  matcher: ["/((?!api|runtime|content|_next/static|_next/image|icon.svg).*)"],
};
