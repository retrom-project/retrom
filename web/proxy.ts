import { randomBytes } from "node:crypto";
import { NextResponse, type NextRequest } from "next/server";
import { decideAuthRoute } from "@/features/auth/routing";
import type { AuthContext } from "@/features/auth/types";
import { loadPlayerFrameSource } from "@/lib/server-web-config";

const backend = process.env.NEXT_BACKEND_ORIGIN ?? "http://127.0.0.1:8080";

function secure(response: NextResponse, policy: string) {
  response.headers.set("Content-Security-Policy", policy);
  response.headers.set("Referrer-Policy", "no-referrer");
  response.headers.set("X-Content-Type-Options", "nosniff");
  response.headers.set("Cross-Origin-Opener-Policy", "same-origin");
  response.headers.set("Cross-Origin-Embedder-Policy", "require-corp");
  response.headers.set("Cache-Control", "private, no-store");
  return response;
}

function documentPolicy(nonce: string, frameSource: string) {
  const developmentEval = process.env.NODE_ENV === "development" ? " 'unsafe-eval'" : "";
  return [
    "default-src 'self'",
    "base-uri 'none'",
    "object-src 'none'",
    "form-action 'self'",
    "frame-ancestors 'self'",
    `script-src 'self' 'nonce-${nonce}' blob: 'wasm-unsafe-eval'${developmentEval}`,
    "style-src 'self' 'unsafe-inline'",
    "connect-src 'self' blob:",
    "worker-src 'self' blob:",
    `frame-src ${frameSource}`,
    "img-src 'self' data: blob:",
    "media-src 'self' blob:",
    "font-src 'self' data:"
  ].join("; ");
}

export async function proxy(request: NextRequest) {
  const nonce = randomBytes(16).toString("base64");
  let context: AuthContext;
  let frameSource: string;
  try {
    const [authResponse, source] = await Promise.all([
      fetch(`${backend}/api/v1/auth/context`, {
        cache: "no-store", redirect: "error", signal: AbortSignal.timeout(10_000),
        headers: { Accept: "application/json", Cookie: request.headers.get("cookie") ?? "" }
      }),
      loadPlayerFrameSource(backend)
    ]);
    if (!authResponse.ok) {throw new Error("AUTH_CONTEXT_UNAVAILABLE");}
    context = await authResponse.json() as AuthContext;
    frameSource = source;
  } catch {
    return secure(new NextResponse("Retrom service unavailable", { status: 503 }), documentPolicy(nonce, "'self'"));
  }
  const policy = documentPolicy(nonce, frameSource);
  const headers = new Headers(request.headers);
  headers.set("x-nonce", nonce);
  headers.set("content-security-policy", policy);
  const returnTo = `${request.nextUrl.pathname}${request.nextUrl.search}`;
  const decision = decideAuthRoute(context, request.nextUrl.pathname, returnTo);
  if (decision.kind === "redirect") {
    return secure(NextResponse.redirect(new URL(decision.destination, request.url)), policy);
  }
  if (decision.kind === "forbidden") {
    return secure(NextResponse.rewrite(new URL("/forbidden", request.url), { request: { headers } }), policy);
  }
  return secure(NextResponse.next({ request: { headers } }), policy);
}

export const config = {
  matcher: ["/((?!api|health|content|runtime|_next/static|_next/image|favicon.ico|icon\\.svg$).*)"]
};
