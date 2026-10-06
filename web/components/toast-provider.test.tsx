import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { FlashToast, ToastProvider, useToast } from "./toast-provider";

import { queueFlashToast } from "./flash-toast";

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

it("replaces a still-visible error when the destination consumes a success message", async () => {
  vi.useFakeTimers();
  const view = render(<ToastProvider><Actions /></ToastProvider>);
  fireEvent.click(screen.getByRole("button", { name: "报错" }));
  await act(() => vi.advanceTimersByTimeAsync(1_000));
  queueFlashToast({ tone: "good", message: "游戏已成功发布" });
  view.rerender(<ToastProvider><Actions /><FlashToast /></ToastProvider>);
  expect(document.querySelectorAll(".app-toast")).toHaveLength(1);
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.getByRole("status")).toHaveTextContent("游戏已成功发布");
  expect(sessionStorage.getItem("retrom:flash-toast")).toBeNull();
  await act(() => vi.advanceTimersByTimeAsync(2_999));
  expect(screen.getByRole("status")).toBeVisible();
  await act(() => vi.advanceTimersByTimeAsync(1));
  expect(screen.queryByRole("status")).toBeNull();
});

it("keeps an async action alive without duplicate submissions or dismissing its replacement", async () => {
  vi.useFakeTimers();
  let complete!: () => void;
  const work = vi.fn(() => new Promise<void>((resolve) => { complete = resolve; }));
  function Action() {
    const { notify } = useToast();
    return <>
      <button onClick={() => notify({ message: "已取消", tone: "good", durationMs: 2_000, action: { label: "撤销", onPress: work } })}>取消</button>
      <button onClick={() => notify({ message: "新操作完成", tone: "good" })}>新操作</button>
    </>;
  }
  render(<ToastProvider><Action /></ToastProvider>);
  fireEvent.click(screen.getByRole("button", { name: "取消" }));
  await act(() => vi.advanceTimersByTimeAsync(1_999));
  const undo = screen.getByRole("button", { name: "撤销" });
  fireEvent.click(undo); fireEvent.click(undo);
  expect(work).toHaveBeenCalledOnce(); expect(undo).toBeDisabled();
  await act(() => vi.advanceTimersByTimeAsync(3_000));
  expect(undo).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "新操作" }));
  await act(() => { complete(); });
  await act(() => vi.advanceTimersByTimeAsync(2_999));
  expect(screen.getByRole("status")).toHaveTextContent("新操作完成");
  await act(() => vi.advanceTimersByTimeAsync(1));
  expect(screen.queryByRole("status")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "取消" }));
  await act(() => vi.advanceTimersByTimeAsync(2_000));
  expect(screen.queryByRole("button", { name: "撤销" })).toBeNull();
});
