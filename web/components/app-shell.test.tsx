import { cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AppShell } from "./app-shell";

const state = vi.hoisted(() => ({
  pathname: "/games/shared-game", search: "", replace: vi.fn(),
  context: { initialized: true, user: null as null | { role: string; displayName: string } },
}));
vi.mock("next/navigation", () => ({
  usePathname: () => state.pathname,
  useSearchParams: () => new URLSearchParams(state.search),
  useRouter: () => ({ replace: state.replace }),
}));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: state.context, error: "" }) }));
vi.mock("@/lib/use-phone-layout", () => ({ usePhoneLayout: () => false }));

beforeEach(() => {
  state.pathname = "/games/shared-game";
  state.search = "";
  state.context = { initialized: true, user: null };
  window.history.replaceState(null, "", state.pathname);
});
afterEach(() => { vi.unstubAllGlobals(); cleanup(); vi.clearAllMocks(); window.history.replaceState(null, "", "/"); });

it("preserves the protected path, query and fragment when opening login", () => {
  state.search = "from=shared&filter=%E4%B8%AD%E6%96%87";
  const destination = `${state.pathname}?${state.search}#details`;
  window.history.replaceState(null, "", destination);
  render(<AppShell>Protected game</AppShell>);
  expect(state.replace).toHaveBeenCalledWith(`/login?returnTo=${encodeURIComponent(destination)}`);
});

it("uses the login route snapshot even when browser history has advanced to the destination", () => {
  const destination = "/games/shared-game?from=shared#details";
  state.pathname = "/login";
  state.search = `returnTo=${encodeURIComponent(destination)}`;
  state.context = { initialized: true, user: { role: "user", displayName: "Player" } };
  window.history.replaceState(null, "", destination);
  const replaceDocument = vi.fn();
  vi.stubGlobal("location", { replace: replaceDocument });
  render(<AppShell>Login</AppShell>);
  expect(replaceDocument).toHaveBeenCalledWith(destination);
  expect(replaceDocument).not.toHaveBeenCalledWith("/");
});

it("leaves failed or pending logins on the same login page", () => {
  state.pathname = "/login";
  state.search = "returnTo=%2Fgames%2Fshared-game";
  render(<AppShell>Login</AppShell>);
  expect(state.replace).not.toHaveBeenCalled();
});

it("opens the home page when login has no return destination", () => {
  state.pathname = "/login";
  state.context = { initialized: true, user: { role: "user", displayName: "Player" } };
  window.history.replaceState(null, "", "/login");
  const replaceDocument = vi.fn();
  vi.stubGlobal("location", { replace: replaceDocument });
  render(<AppShell>Login</AppShell>);
  expect(replaceDocument).toHaveBeenCalledWith("/");
});

it("leaves the anonymous route cache behind after authentication", () => {
  const destination = "/games/shared-game?from=shared#details";
  const replaceDocument = vi.fn();
  state.pathname = "/login";
  state.search = `returnTo=${encodeURIComponent(destination)}`;
  state.context = { initialized: true, user: { role: "user", displayName: "Player" } };
  vi.stubGlobal("location", { replace: replaceDocument });
  render(<AppShell>Login</AppShell>);
  expect(replaceDocument).toHaveBeenCalledWith(destination);
  expect(state.replace).not.toHaveBeenCalled();
});
