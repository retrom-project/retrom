import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { InfiniteScrollFooter } from "./infinite-scroll-footer";

const callbacks: IntersectionObserverCallback[] = [];
const disconnect = vi.fn();
beforeEach(() => {
  callbacks.length = 0;
  disconnect.mockClear();
  vi.stubGlobal("IntersectionObserver", class {
    constructor(callback: IntersectionObserverCallback) { callbacks.push(callback); }
    observe() {}
    disconnect = disconnect;
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
function intersect(isIntersecting: boolean) {
  act(() => callbacks.at(-1)!([{ isIntersecting } as IntersectionObserverEntry], {} as IntersectionObserver));
}

it("automatically requests the next page near the bottom and reobserves the growing list", () => {
  const loadMore = vi.fn();
  const props = { count: 24, total: 53, hasMore: true, loadingMore: false, moreError: "", loadMore };
  const view = render(<InfiniteScrollFooter {...props} />);
  intersect(false);
  expect(loadMore).not.toHaveBeenCalled();
  intersect(true);
  expect(loadMore).toHaveBeenCalledTimes(1);
  view.rerender(<InfiniteScrollFooter {...props} loadingMore />);
  expect(disconnect).toHaveBeenCalledOnce();
  expect(screen.getByRole("button", { name: "加载更多" })).toBeDisabled();
  view.rerender(<InfiniteScrollFooter {...props} count={48} />);
  expect(callbacks).toHaveLength(2);
  intersect(true);
  expect(loadMore).toHaveBeenCalledTimes(2);
  view.rerender(<InfiniteScrollFooter {...props} count={53} hasMore={false} />);
  expect(screen.getByRole("status")).toHaveTextContent("已加载 53 / 53 · 已加载全部");
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
  expect(callbacks).toHaveLength(2);
});

it("does not automatically retry failed reads and keeps a keyboard-accessible retry", () => {
  const loadMore = vi.fn();
  render(<InfiniteScrollFooter count={24} total={53} hasMore loadingMore={false} moreError="network" loadMore={loadMore} />);
  expect(callbacks).toHaveLength(0);
  expect(screen.getByRole("alert")).toHaveTextContent("network");
  fireEvent.click(screen.getByRole("button", { name: "重试加载" }));
  expect(loadMore).toHaveBeenCalledOnce();
});
