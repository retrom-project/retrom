import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { PwaProvider } from "./pwa-provider";
import { PwaNotice, PwaSettings } from "./pwa-controls";

beforeEach(() => {
  vi.stubGlobal("isSecureContext", true);
  vi.stubGlobal("matchMedia", () => Object.assign(new EventTarget(), { matches: false }));
  vi.stubGlobal("navigator", { userAgent: "Chrome", platform: "Linux", maxTouchPoints: 0, onLine: true });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
const view = () => render(<PwaProvider><PwaNotice /><PwaSettings /></PwaProvider>);

test("offers installation only after the browser supplies a usable prompt", async () => {
  view();
  expect(screen.queryByRole("button", { name: "安装 Retrom" })).not.toBeInTheDocument();
  const prompt = vi.fn(async () => {});
  const event = Object.assign(new Event("beforeinstallprompt", { cancelable: true }), {
    prompt, userChoice: Promise.resolve({ outcome: "accepted" }),
  });
  act(() => { window.dispatchEvent(event); });
  expect(event.defaultPrevented).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "安装 Retrom" }));
  await screen.findByText("请完成浏览器的安装提示。");
  expect(prompt).toHaveBeenCalledOnce();
  expect(screen.queryByText("已安装")).not.toBeInTheDocument();
  act(() => { window.dispatchEvent(new Event("appinstalled")); });
  expect(screen.getByText("已安装")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "安装 Retrom" })).not.toBeInTheDocument();
});

test("iOS explains the share menu instead of offering a nonfunctional install button", async () => {
  vi.stubGlobal("navigator", { userAgent: "iPhone", platform: "iPhone", maxTouchPoints: 5, onLine: true });
  view();
  expect(await screen.findByText(/分享菜单/u)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "安装 Retrom" })).not.toBeInTheDocument();
});

test("browsers without Service Worker support can continue online without a perpetual loading state", async () => {
  view();
  expect(await screen.findByText("当前浏览器不支持离线提示，仍可联网使用。")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "检查更新" })).not.toBeInTheDocument();
  expect(screen.queryByText("正在准备离线提示…")).not.toBeInTheDocument();
});

test("standalone launch is recognized and a disconnected connection is visible", async () => {
  vi.stubGlobal("matchMedia", () => Object.assign(new EventTarget(), { matches: true }));
  const browser = { userAgent: "Chrome", platform: "Linux", maxTouchPoints: 0, onLine: true };
  vi.stubGlobal("navigator", browser);
  view();
  expect(await screen.findByText("已安装")).toBeInTheDocument();
  act(() => { browser.onLine = false; window.dispatchEvent(new Event("offline")); });
  // The notice floats in its own layer instead of pushing the page down.
  expect(screen.getByText(/当前网络已断开/u).closest(".pwa-floating")).not.toBeNull();
  act(() => { browser.onLine = true; window.dispatchEvent(new Event("online")); });
  await waitFor(() => expect(screen.queryByText(/当前网络已断开/u)).not.toBeInTheDocument());
});
