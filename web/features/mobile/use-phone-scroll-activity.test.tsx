import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { usePhoneScrollActivity } from "./use-phone-scroll-activity";

const layout = vi.hoisted(() => ({ phone: true }));
vi.mock("./phone-layout", () => ({ usePhoneLayout: () => layout.phone }));

beforeEach(() => { layout.phone = true; vi.useFakeTimers(); });
afterEach(() => { cleanup(); vi.useRealTimers(); });

function rail() {
  return Object.defineProperties(document.createElement("div"), {
    clientWidth: { value: 200 }, scrollWidth: { value: 800 },
  });
}

it("starts hidden and hides only after scrolling has stopped for 900ms", () => {
  const element = rail();
  const { result, unmount } = renderHook(usePhoneScrollActivity);
  expect(result.current.scrolling).toBe(false);
  act(() => result.current.onScroll(element));
  expect(result.current.scrolling).toBe(true);
  expect(result.current.thumb).toEqual({ width: 25, left: 0 });
  act(() => { vi.advanceTimersByTime(800); });
  element.scrollLeft = 600;
  act(() => result.current.onScroll(element));
  expect(result.current.thumb).toEqual({ width: 25, left: 75 });
  act(() => { vi.advanceTimersByTime(800); });
  expect(result.current.scrolling).toBe(true);
  act(() => { vi.advanceTimersByTime(100); });
  expect(result.current.scrolling).toBe(false);
  act(() => result.current.onScroll(element));
  unmount();
  expect(vi.getTimerCount()).toBe(0);
});

it("leaves scrolling outside the phone layout unchanged", () => {
  layout.phone = false;
  const { result } = renderHook(usePhoneScrollActivity);
  act(() => result.current.onScroll(rail()));
  expect(result.current.scrolling).toBe(false);
  expect(vi.getTimerCount()).toBe(0);
});

it("keeps the indicator hidden when there is no horizontal overflow", () => {
  const { result } = renderHook(usePhoneScrollActivity);
  act(() => result.current.onScroll(document.createElement("div")));
  expect(result.current.scrolling).toBe(false);
  expect(vi.getTimerCount()).toBe(0);
});
