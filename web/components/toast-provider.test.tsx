import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ToastProvider, useToast } from "./toast-provider";

function Actions() {
  const { notify, clear } = useToast();
  return <div data-testid="card">
    <button onClick={() => notify({ tone: "bad", message: "启动失败" })}>报错</button>
    <button onClick={clear}>重试</button>
  </div>;
}

afterEach(() => { cleanup(); vi.useRealTimers(); });

it("keeps feedback inside the fullscreen element without moving focus", async () => {
  const view = render(<ToastProvider><div data-testid="fullscreen"><Actions /></div></ToastProvider>);
  const fullscreen = screen.getByTestId("fullscreen");
  const button = screen.getByRole("button", { name: "报错" }); button.focus();
  const original = Object.getOwnPropertyDescriptor(document, "fullscreenElement");
  let target: Element | null = fullscreen;
  Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => target });
  await act(() => { document.dispatchEvent(new Event("fullscreenchange")); });
  fireEvent.click(button);
  expect(fullscreen.querySelector(".app-toast")).toBe(screen.getByRole("alert")); expect(button).toHaveFocus();
  target = null;
  await act(() => { document.dispatchEvent(new Event("fullscreenchange")); });
  expect(fullscreen.querySelector(".app-toast")).toBeNull(); expect(document.body.querySelector(".app-toast")).not.toBeNull();
  if (original) {Object.defineProperty(document, "fullscreenElement", original);} else {Reflect.deleteProperty(document, "fullscreenElement");}
  view.unmount();
});

it("replaces repeated errors with one toast and gives the latest error three seconds", async () => {
  vi.useFakeTimers();
  render(<ToastProvider><Actions /></ToastProvider>);
  fireEvent.click(screen.getByRole("button", { name: "报错" }));
  expect(screen.getByTestId("card").querySelector("[role=alert]")).toBeNull();
  await act(() => vi.advanceTimersByTimeAsync(2_000));
  fireEvent.click(screen.getByRole("button", { name: "报错" }));
  expect(screen.getAllByRole("alert")).toHaveLength(1);
  await act(() => vi.advanceTimersByTimeAsync(2_999));
  expect(screen.getByRole("alert")).toBeVisible();
  await act(() => vi.advanceTimersByTimeAsync(1));
  expect(screen.queryByRole("alert")).toBeNull();
});

it("clears the existing notification before a retry", () => {
  render(<ToastProvider><Actions /></ToastProvider>);
  fireEvent.click(screen.getByRole("button", { name: "报错" }));
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  expect(screen.queryByRole("alert")).toBeNull();
});
