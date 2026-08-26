import { randomBytes } from "node:crypto";
import { NextResponse, type NextRequest } from "next/server";
import { decideAuthRoute } from "@/features/auth/routing";
import type { AuthContext } from "@/features/auth/types";
import { playerFrameSource } from "@/lib/rpg-runtime-csp";

const backend = process.env.NEXT_BACKEND_ORIGIN ?? "http://127.0.0.1:8080";

function secure(response: NextResponse, policy: string) {
  response.headers.set("Content-Security-Policy", policy);
  response.headers.set("Referrer-Policy", "no-referrer");
  response.headers.set("X-Content-Type-Options", "nosniff");
  response.headers.set("Cross-Origin-Opener-Policy", "same-origin");
  response.headers.set("Cross-Origin-Embedder-Policy", "require-corp");
  return response;
}

export async function proxy(request: NextRequest) {
  const nonce = randomBytes(16).toString("base64");
  const developmentEval = process.env.NODE_ENV === "development" ? " 'unsafe-eval'" : "";
  const policy = [
    "default-src 'self'",
    "base-uri 'none'",
    "object-src 'none'",
    "form-action 'self'",
    "frame-ancestors 'self'",
    `script-src 'self' 'nonce-${nonce}' blob: 'wasm-unsafe-eval'${developmentEval}`,
    "style-src 'self' 'unsafe-inline'",
    "connect-src 'self' blob:",
    "worker-src 'self' blob:",
    `frame-src ${playerFrameSource(request.nextUrl.pathname, process.env.RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE)}`,
    "img-src 'self' data: blob:",
    "media-src 'self' blob:",
    "font-src 'self' data:"
  ].join("; ");
  const headers = new Headers(request.headers);
  headers.set("x-nonce", nonce);
  headers.set("content-security-policy", policy);
  const authResponse = await fetch(`${backend}/api/v1/auth/context`, {
    cache: "no-store",
    headers: { Accept: "application/json", Cookie: request.headers.get("cookie") ?? "" }
  });
  if (!authResponse.ok) {
    return secure(new NextResponse("Retrom authentication service unavailable", { status: 503 }), policy);
  }
  const context = await authResponse.json() as AuthContext;
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
  matcher: ["/((?!api|health|content|runtime|_next/static|_next/image|favicon.ico).*)"]
};
