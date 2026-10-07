import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ToastProvider, useToast } from "./toast-provider";

function Actions() {
  const { notify, clear } = useToast();
  return <div data-testid="actions">
    <button onClick={() => notify({ tone: "bad", message: "启动失败" })}>报错</button>
    <button onClick={() => notify({ tone: "good", message: "保存完成" })}>保存</button>
    <button onClick={clear}>重试</button>
  </div>;
}
afterEach(() => { cleanup(); vi.useRealTimers(); });

it("replaces repeated feedback and gives the latest notification three seconds", async () => {
  vi.useFakeTimers();
  render(<ToastProvider><Actions /></ToastProvider>);
  fireEvent.click(screen.getByRole("button", { name: "报错" }));
  expect(screen.getByRole("alert")).toHaveAttribute("aria-live", "assertive");
  await act(() => vi.advanceTimersByTimeAsync(2_000));
  fireEvent.click(screen.getByRole("button", { name: "报错" }));
  expect(screen.getAllByRole("alert")).toHaveLength(1);
  await act(() => vi.advanceTimersByTimeAsync(2_999));
  expect(screen.getByRole("alert")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.getByRole("status")).toHaveAttribute("aria-live", "polite");
  await act(() => vi.advanceTimersByTimeAsync(2_999));
  expect(screen.getByRole("status")).toHaveTextContent("保存完成");
  await act(() => vi.advanceTimersByTimeAsync(1));
  expect(screen.queryByRole("status")).toBeNull();
});

it("supports explicit dismissal and returns focus to the originating control", () => {
  render(<ToastProvider><Actions /></ToastProvider>);
  const trigger = screen.getByRole("button", { name: "报错" });
  trigger.focus(); fireEvent.click(trigger);
  expect(trigger).toHaveFocus();
  const close = screen.getByRole("button", { name: "关闭通知" });
  close.focus(); fireEvent.click(close);
  expect(screen.queryByRole("alert")).toBeNull();
  expect(trigger).toHaveFocus();
  fireEvent.click(trigger);
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  expect(screen.queryByRole("alert")).toBeNull();
});

it("retains feedback across content navigation and moves its portal with fullscreen", async () => {
  const view = render(<ToastProvider><div data-testid="fullscreen"><Actions /></div></ToastProvider>);
  const fullscreen = screen.getByTestId("fullscreen");
  const original = Object.getOwnPropertyDescriptor(document, "fullscreenElement");
  let target: Element | null = fullscreen;
  Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => target });
  try {
    await act(() => { document.dispatchEvent(new Event("fullscreenchange")); });
    const trigger = screen.getByRole("button", { name: "报错" });
    trigger.focus(); fireEvent.click(trigger);
    expect(fullscreen.querySelector(".app-toast")).toBe(screen.getByRole("alert"));
    expect(trigger).toHaveFocus();
    target = null;
    await act(() => { document.dispatchEvent(new Event("fullscreenchange")); });
    expect(fullscreen.querySelector(".app-toast")).toBeNull();
    view.rerender(<ToastProvider><p>目标页面</p></ToastProvider>);
    expect(screen.getByRole("alert")).toHaveTextContent("启动失败");
  } finally {
    if (original) { Object.defineProperty(document, "fullscreenElement", original); }
    else { Reflect.deleteProperty(document, "fullscreenElement"); }
  }
});
