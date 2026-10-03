import { afterEach, expect, it, vi } from "vitest";
import { readTagRange } from "./tag-pages";

afterEach(() => vi.unstubAllGlobals());

it("reloads every previously loaded page without using a pre-mutation cursor", async () => {
  const first = Array.from({ length: 100 }, (_, index) => ({ tagId: `id-${index}` }));
  const second = [{ tagId: "id-100" }];
  const fetchMock = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ items: first, nextCursor: "fresh-100", summary: {} })))
    .mockResolvedValueOnce(new Response(JSON.stringify({ items: second, nextCursor: "fresh-101", summary: {} })));
  vi.stubGlobal("fetch", fetchMock);
  const result = await readTagRange({ q: "", status: "ALL", sort: "NAME_ASC" }, 101);
  expect(result.items.map((item) => item.tagId)).toEqual([...first, ...second].map((item) => item.tagId));
  expect(result.nextCursor).toBe("fresh-101");
  expect(fetchMock.mock.calls[0][0]).not.toContain("cursor=");
  expect(fetchMock.mock.calls[1][0]).toContain("cursor=fresh-100");
});
