import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import { PwaProvider } from "./pwa-provider";
import { PwaNotice, PwaSettings } from "./pwa-controls";

beforeEach(() => {
  vi.stubGlobal("isSecureContext", true);
  vi.stubGlobal("matchMedia", () => Object.assign(new EventTarget(), { matches: false }));
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function environment() {
  const registration = Object.assign(new EventTarget(), {
    active: {}, waiting: null as object | null, installing: null as object | null, update: vi.fn(async () => {}),
  });
  const serviceWorker = Object.assign(new EventTarget(), { register: vi.fn(async () => registration) });
  vi.stubGlobal("navigator", { serviceWorker, userAgent: "Chrome", platform: "Linux", maxTouchPoints: 0, onLine: true });
  return registration;
}

async function view() {
  await act(async () => { render(<PwaProvider><PwaNotice /><PwaSettings /></PwaProvider>); });
}

test("a manual check with no new worker confirms the latest version in a toast", async () => {
  const registration = environment();
  await view();
  expect(screen.queryByText("当前已经是最新版本了。")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "检查更新" }));
  await waitFor(() => expect(registration.update).toHaveBeenCalledOnce());
  expect((await screen.findByText("当前已经是最新版本了。")).closest(".app-toast")).not.toBeNull();
});

test("keeps the check button busy until the browser finishes checking", async () => {
  const registration = environment();
  let finish!: () => void;
  registration.update.mockImplementation(() => new Promise(resolve => { finish = resolve; }));
  await view();
  fireEvent.click(screen.getByRole("button", { name: "检查更新" }));
  expect(screen.getByRole("button", { name: "正在检查…" })).toBeDisabled();
  await act(async () => finish());
  expect(await screen.findByRole("button", { name: "检查更新" })).toBeEnabled();
});

test("reports a waiting version without claiming the app is current", async () => {
  const registration = environment();
  registration.update.mockImplementation(async () => { registration.waiting = {}; });
  await view();
  fireEvent.click(screen.getByRole("button", { name: "检查更新" }));
  expect(await screen.findByRole("button", { name: "点击更新" })).toBeVisible();
  expect((await screen.findByText("发现新版本，请点击顶部的“点击更新”。")).closest(".app-toast")).not.toBeNull();
  expect(screen.queryByText("当前已经是最新版本了。")).not.toBeInTheDocument();
});

test("does not report the latest version while a new worker is installing", async () => {
  const registration = environment();
  registration.update.mockImplementation(async () => { registration.installing = {}; });
  await view();
  fireEvent.click(screen.getByRole("button", { name: "检查更新" }));
  expect((await screen.findByText("正在准备更新，请稍候。")).closest(".app-toast")).not.toBeNull();
  expect(screen.queryByText("当前已经是最新版本了。")).not.toBeInTheDocument();
});

test("shows a failed check and allows retrying instead of claiming success", async () => {
  const registration = environment();
  registration.update.mockRejectedValueOnce(new TypeError("offline"));
  await view();
  fireEvent.click(screen.getByRole("button", { name: "检查更新" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("检查更新失败，请稍后重试。");
  expect(screen.queryByText("当前已经是最新版本了。")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "检查更新" }));
  expect(await screen.findByText("当前已经是最新版本了。")).toBeVisible();
});
