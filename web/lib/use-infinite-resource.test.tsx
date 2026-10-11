import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useInfiniteResource } from "./use-infinite-resource";
import type { PageRequest } from "./use-infinite-resource";

afterEach(cleanup);
const key = (item: { id: string }) => item.id;
function page(offset: number, total = 53) {
  return { items: Array.from({ length: Math.min(24, Math.max(0, total - offset)) }, (_, index) => ({ id: String(index + offset) })), total, offset, limit: 24, gameCount: 7 };
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((accept) => { resolve = accept; });
  return { promise, resolve };
}

it("loads bounded pages only when requested, appends once and stops at the real total", async () => {
  const loader = vi.fn(async ({ offset }: PageRequest) => page(offset));
  const { result } = renderHook(() => useInfiniteResource(loader, key));
  await waitFor(() => expect(result.current.data?.items).toHaveLength(24));
  expect(loader).toHaveBeenCalledTimes(1);
  await act(async () => { result.current.loadMore(); result.current.loadMore(); });
  expect(result.current.data?.items).toHaveLength(48);
  expect(loader).toHaveBeenCalledTimes(2);
  await act(async () => result.current.loadMore());
  expect(result.current.data).toMatchObject({ total: 53, gameCount: 7 });
  expect(result.current.data?.items).toHaveLength(53);
  expect(result.current.hasMore).toBe(false);
  await act(async () => result.current.loadMore());
  expect(loader.mock.calls.map(([request]) => [request.offset, request.limit])).toEqual([[0, 24], [24, 24], [48, 24]]);
});

it("keeps loaded cards on append failure, retries the same page and deduplicates overlapping results", async () => {
  const loader = vi.fn<({ offset }: PageRequest) => Promise<ReturnType<typeof page>>>()
    .mockResolvedValueOnce(page(0)).mockRejectedValueOnce(new Error("network"))
    .mockResolvedValueOnce({ ...page(24), items: [{ id: "23" }, ...page(24).items.slice(1)] })
    .mockResolvedValueOnce(page(48));
  const { result } = renderHook(() => useInfiniteResource(loader, key));
  await waitFor(() => expect(result.current.data?.items).toHaveLength(24));
  await act(async () => result.current.loadMore());
  expect(result.current.data?.items).toHaveLength(24);
  expect(result.current.error).toBe("");
  expect(result.current.moreError).toBe("network");
  await act(async () => result.current.loadMore());
  expect(result.current.data?.items).toHaveLength(47);
  expect(result.current.moreError).toBe("");
  await act(async () => result.current.loadMore());
  expect(loader.mock.calls.map(([request]) => request.offset)).toEqual([0, 24, 24, 48]);
});

it("aborts obsolete filters and cannot mix their late response into the new list", async () => {
  const old = deferred<ReturnType<typeof page>>();
  const first = vi.fn(({ signal }: PageRequest) => { expect(signal.aborted).toBe(false); return old.promise; });
  const second = vi.fn(async () => ({ ...page(0, 1), items: [{ id: "new-filter" }] }));
  const { result, rerender, unmount } = renderHook(({ loader }) => useInfiniteResource(loader, key), { initialProps: { loader: first } });
  rerender({ loader: second });
  expect(first.mock.calls[0][0].signal.aborted).toBe(true);
  await waitFor(() => expect(result.current.data?.items[0].id).toBe("new-filter"));
  await act(async () => old.resolve(page(0)));
  expect(result.current.data?.items).toEqual([{ id: "new-filter" }]);
  unmount();
});

it("refreshes the loaded range after deletion without discarding the visible list or raising the limit", async () => {
  let deleted = false;
  const loader = vi.fn(async ({ offset }: PageRequest) => {
    const response = page(offset, deleted ? 52 : 53);
    return { ...response, items: response.items.map((item) => ({ id: String(Number(item.id) + (deleted ? 1 : 0)) })) };
  });
  const { result } = renderHook(() => useInfiniteResource(loader, key));
  await waitFor(() => expect(result.current.data?.items).toHaveLength(24));
  await act(async () => result.current.loadMore());
  deleted = true;
  await act(async () => {
    result.current.reload();
    expect(result.current.data?.items).toHaveLength(48);
  });
  expect(result.current.data?.items).toHaveLength(48);
  expect(result.current.data?.items[0].id).toBe("1");
  expect(result.current.data?.total).toBe(52);
  expect(loader.mock.calls.map(([request]) => [request.offset, request.limit])).toEqual([[0, 24], [24, 24], [0, 24], [24, 24]]);
});

it("stops on an empty page even if a concurrent change leaves an outdated total", async () => {
  const loader = vi.fn(async () => ({ ...page(0), items: [] }));
  const { result } = renderHook(() => useInfiniteResource(loader, key));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.hasMore).toBe(false);
  await act(async () => result.current.loadMore());
  expect(loader).toHaveBeenCalledTimes(1);
});

it("retries the initial page after a failure", async () => {
  const loader = vi.fn<(request: PageRequest) => Promise<ReturnType<typeof page>>>().mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce(page(0, 1));
  const { result } = renderHook(() => useInfiniteResource(loader, key));
  await waitFor(() => expect(result.current.error).toBe("offline"));
  await act(async () => result.current.reload());
  expect(result.current.error).toBe("");
  expect(result.current.data?.items).toHaveLength(1);
});
