import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useLibraryQuery } from "./use-library-query";
vi.mock("next/navigation", () => ({
  usePathname: () => window.location.pathname,
  useRouter: () => ({ replace: (url: string) => window.history.replaceState(null, "", url) }),
}));
it("preserves the review scan context across filters and pagination without sending it as a game filter", () => {
  window.history.replaceState(null, "", "/admin/reviews?scanId=current");
  const { result } = renderHook(() => useLibraryQuery({ scanId: "current" }));
  act(() => result.current.update({ q: "NES", tag: "tag" }));
  expect(new URLSearchParams(window.location.search).get("scanId")).toBe("current");
  act(() => result.current.update({ offset: 24 }, false));
  expect(new URLSearchParams(window.location.search).get("scanId")).toBe("current");
  expect(result.current.query).toMatchObject({ q: "NES", tagId: "tag", offset: 24 });
  expect(result.current.query).not.toHaveProperty("scanId");
});
