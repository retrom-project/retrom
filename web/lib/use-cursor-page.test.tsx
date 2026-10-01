import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useCursorPage } from "./use-cursor-page";

const { fetchPage } = vi.hoisted(() => ({ fetchPage: vi.fn() }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ authenticatedFetch: fetchPage }) }));

const initial = { items: ["first"], nextCursor: "next", filteredCount: 12, summary: { total: 12 } };
afterEach(() => { vi.clearAllMocks(); vi.useRealTimers(); });

describe("cursor page requests", () => {
  it("does not refetch the server page and keeps full stats across bounded navigation", async () => {
    fetchPage.mockResolvedValue({ ok: true, json: async () => ({ items: ["second"], nextCursor: null }) });
    const { result } = renderHook(() => useCursorPage(initial, "/api/list?limit=6"));
    expect(fetchPage).not.toHaveBeenCalled();
    act(() => result.current.next());
    await waitFor(() => expect(result.current.index).toBe(1));
    expect(result.current.page).toEqual({ ...initial, items: ["second"], nextCursor: null });
    expect(fetchPage.mock.calls[0][0]).toBe("/api/list?limit=6&cursor=next");
    fetchPage.mockResolvedValue({ ok: true, json: async () => initial });
    act(() => result.current.previous());
    await waitFor(() => expect(result.current.index).toBe(0));
    expect(result.current.page.items).toEqual(["first"]);
  });

  it("aborts in-flight pages and ignores stale results after changing filters", async () => {
    let resolveOld!: (value: unknown) => void;
    fetchPage.mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve; }));
    fetchPage.mockResolvedValue({ ok: true, json: async () => ({ ...initial, items: ["filtered"], nextCursor: null, filteredCount: 1 }) });
    const { result, rerender } = renderHook(({ url }) => useCursorPage(initial, url), { initialProps: { url: "/api/list?limit=6" } });
    act(() => result.current.next());
    const oldSignal = fetchPage.mock.calls[0][1].signal as AbortSignal;
    rerender({ url: "/api/list?limit=6&q=filtered" });
    expect(oldSignal.aborted).toBe(true);
    await waitFor(() => expect(result.current.page.items).toEqual(["filtered"]));
    await act(async () => { resolveOld({ ok: true, json: async () => ({ items: ["stale"], nextCursor: null }) }); });
    expect(result.current.page.items).toEqual(["filtered"]);
    expect(result.current.index).toBe(0);
  });
});
