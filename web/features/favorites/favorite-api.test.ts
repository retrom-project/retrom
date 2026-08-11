import { describe, expect, it, vi } from "vitest";
import { createFavoriteFolder, deleteFavoriteFolder, FavoriteAPIError } from "./favorite-api";

describe("favorite API client", () => {
  it("adds principal-scoped mutation headers and exact ETags", async () => {
    const fetch = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(async () => new Response(null, { status: 204 }));
    await deleteFavoriteFolder(fetch, "01980000-0000-7000-8000-000000000001", 7);
    const init = fetch.mock.calls[0][1];
    const headers = new Headers(init?.headers);
    expect(init?.method).toBe("DELETE");
    expect(init?.body).toBe("{}");
    expect(headers.get("If-Match")).toBe('"v7"');
    expect(headers.get("Idempotency-Key")).toMatch(/^[0-9a-f-]{36}$/);
  });

  it("preserves stable API error details", async () => {
    const fetch = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(async () => new Response(JSON.stringify({ error: { code: "FAVORITE_FOLDER_NAME_CONFLICT", message: "duplicate", requestId: "request-1" } }), { status: 409, headers: { "Content-Type": "application/json" } }));
    await expect(createFavoriteFolder(fetch, "same", [])).rejects.toEqual(expect.objectContaining<Partial<FavoriteAPIError>>({
      status: 409, code: "FAVORITE_FOLDER_NAME_CONFLICT", message: "duplicate", requestId: "request-1",
    }));
  });
});
