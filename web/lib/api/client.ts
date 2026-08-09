import createClient from "openapi-fetch";
import type { paths } from "./generated/schema";

export const api = createClient<paths>({ baseUrl: "", credentials: "same-origin" });

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
  if (response.status === 401) authenticationFailure?.();
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
