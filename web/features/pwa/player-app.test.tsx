import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { AppShell } from "@/components/app-shell";
import MePage from "@/app/me/page";
import { PwaProvider, usePwa } from "./pwa-provider";

const route = vi.hoisted(() => ({ pathname: "/me", replace: vi.fn(), role: "admin" }));
vi.mock("next/navigation", () => ({ usePathname: () => route.pathname, useRouter: () => ({ replace: route.replace }) }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({
  context: { initialized: true, user: { role: route.role, displayName: "Player" } }, logout: vi.fn(),
}) }));
vi.mock("@/components/service-health", () => ({ ServiceHealth: () => null }));
let mode: EventTarget & { matches: boolean };
beforeEach(() => {
  route.pathname = "/me"; route.role = "admin"; route.replace.mockReset();
  mode = Object.assign(new EventTarget(), { matches: false });
  vi.stubGlobal("matchMedia", (query: string) => query.includes("display-mode") ? mode : Object.assign(new EventTarget(), { matches: false }));
  vi.stubGlobal("navigator", { userAgent: "Chrome", platform: "Linux", maxTouchPoints: 0, onLine: true });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
function WindowMode() {
  return <output aria-label="window mode">{String(usePwa()?.appWindow)}</output>;
}
function view() {
  return render(<PwaProvider><WindowMode /><AppShell><MePage /></AppShell></PwaProvider>);
}

test("PWA hides administrator navigation before detection and after fullscreen transitions", async () => {
  mode.matches = true;
  view();
  expect(screen.queryByRole("link", { name: "管理游戏库" })).not.toBeInTheDocument();
  await waitFor(() => expect(screen.getByLabelText("window mode")).toHaveTextContent("true"));
  expect(screen.getAllByRole("link", { name: "账户设置" })[0]).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "打开主要导航" }));
  expect(screen.getByRole("dialog")).toBeInTheDocument();
  expect(document.querySelector('a[href^="/admin"]')).toBeNull();
  act(() => { mode.matches = false; mode.dispatchEvent(new Event("change")); });
  expect(screen.getByLabelText("window mode")).toHaveTextContent("true");
  expect(document.querySelector('a[href^="/admin"]')).toBeNull();
});

test("installing from a browser tab preserves that tab's management entries", async () => {
  view();
  await screen.findByRole("link", { name: "管理游戏库" });
  act(() => { window.dispatchEvent(new Event("appinstalled")); });
  expect(screen.getByLabelText("window mode")).toHaveTextContent("false");
  expect(screen.getByRole("link", { name: "管理后台" })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "打开主要导航" }));
  expect(screen.getAllByRole("link", { name: "管理后台" })).toHaveLength(2);
});

test.each(["standalone", "ios"])("%s launch redirects admin deep links without mounting the admin page", async (platform) => {
  route.pathname = "/admin/games";
  if (platform === "standalone") { mode.matches = true; }
  else { vi.stubGlobal("navigator", { standalone: true, userAgent: "iPhone", platform: "iPhone", onLine: true }); }
  const mount = vi.fn();
  function AdminPage() { mount(); return <h1>Game management</h1>; }
  render(<PwaProvider><AppShell><AdminPage /></AppShell></PwaProvider>);
  expect(mount).not.toHaveBeenCalled();
  await waitFor(() => expect(route.replace).toHaveBeenCalledWith("/"));
  expect(mount).not.toHaveBeenCalled();
});

test("browser administrators retain deep links while ordinary users have no drawer entry", async () => {
  route.pathname = "/admin/games";
  const app = render(<PwaProvider><AppShell><h1>Game management</h1></AppShell></PwaProvider>);
  await screen.findByRole("heading", { name: "Game management" });
  expect(route.replace).not.toHaveBeenCalled();
  app.unmount();
  route.pathname = "/me"; route.role = "user";
  view();
  await waitFor(() => expect(screen.getByLabelText("window mode")).toHaveTextContent("false"));
  fireEvent.click(screen.getByRole("button", { name: "打开主要导航" }));
  expect(document.querySelector('a[href^="/admin"]')).toBeNull();
});
