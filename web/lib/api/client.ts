import createClient from "openapi-fetch";
import type { paths } from "./generated/schema";

// openapi-fetch builds a Request object before invoking fetch. Browsers resolve
// relative URLs against the current document, while Node (used by Vitest and
// server-side module evaluation) requires an absolute base URL.
const apiBaseUrl = typeof window === "undefined" ? "http://127.0.0.1" : window.location.origin;

export const api = createClient<paths>({
  baseUrl: apiBaseUrl,
  credentials: "same-origin",
  // Resolve fetch at call time so browser polyfills and the Vitest transport
  // stub are both honored instead of capturing a process-global implementation.
  fetch: (request) => globalThis.fetch(request),
});

let csrfToken: string | null = null;
let authenticationFailure: (() => void) | null = null;

export function configureAuthenticatedClient(options: { csrfToken: string | null; onAuthenticationFailure: (() => void) | null }) {
  csrfToken = options.csrfToken;
  authenticationFailure = options.onAuthenticationFailure;
}

export function writeHeaders(extra: Record<string, string> = {}) {
  return csrfToken ? { ...extra, "X-Retrom-Csrf": csrfToken } : extra;
}

export function handleAuthenticationResponse(response: Response) {
  if (response.status === 401) {authenticationFailure?.();}
  return response;
}

api.use({
  onRequest({ request }) {
    if (csrfToken && !["GET", "HEAD", "OPTIONS"].includes(request.method.toUpperCase())) {
      request.headers.set("X-Retrom-Csrf", csrfToken);
    }
  },
  onResponse({ response }) {
    handleAuthenticationResponse(response);
  }
});
