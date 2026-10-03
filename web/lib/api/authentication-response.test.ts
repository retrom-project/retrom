import { afterEach, describe, expect, it, vi } from "vitest";
import { configureAuthenticatedClient, handleAuthenticationResponse } from "./client";

afterEach(() => configureAuthenticatedClient({ csrfToken: null, onAuthenticationFailure: null }));

describe("authentication response semantics", () => {
  it.each([
    [401, "AUTHENTICATION_FAILED", false],
    [401, "LAUNCH_CREDENTIAL_INVALID", false],
    [422, "CURRENT_PASSWORD_INVALID", false],
    [403, "CSRF_VALIDATION_FAILED", false],
    [401, "AUTHENTICATION_REQUIRED", true],
  ])("classifies %s %s without consuming the response", async (status, code, invalidates) => {
    const failure = vi.fn();
    configureAuthenticatedClient({ csrfToken: "csrf", onAuthenticationFailure: failure });
    const response = new Response(JSON.stringify({ error: { code } }), { status });
    expect(await handleAuthenticationResponse(response)).toBe(response);
    expect(failure).toHaveBeenCalledTimes(invalidates ? 1 : 0);
    expect(await response.json()).toEqual({ error: { code } });
  });
});
