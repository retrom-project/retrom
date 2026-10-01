import {afterEach, expect, it, vi} from "vitest";
import {act, cleanup, render, screen, within} from "@testing-library/react";
import {renderToString} from "react-dom/server";
import {hydrateRoot, type Root} from "react-dom/client";
import userEvent from "@testing-library/user-event";
import {ContentLoadingField} from "./content-loading-field";
import {readContentLoading, resolveContentLoading, writeContentLoading} from "./content-loading";

vi.mock("@/features/auth/auth-provider", () => ({useAuth: () => ({context: {user: {userId: "user-1"}}})}));
afterEach(() => {cleanup(); localStorage.clear(); vi.restoreAllMocks();});

it("defaults to demand loading and remembers a device preference for the current user", async () => {
  render(<ContentLoadingField capability="ON_DEMAND_AND_PRELOAD" />);
  const selector = screen.getByRole("combobox", {name: "内容加载"});
  expect(selector).toHaveValue("ON_DEMAND");
  await userEvent.setup().selectOptions(selector, "PRELOAD");
  expect(selector).toHaveValue("PRELOAD");
  expect(readContentLoading("user-1")).toBe("PRELOAD");
  expect(readContentLoading("user-2")).toBe("ON_DEMAND");
});

it("blocks native choices until hydration can persist user preferences", async () => {
  const host = document.createElement("div");
  document.body.append(host);
  let root: Root | undefined;
  try {
    const field = <ContentLoadingField capability="ON_DEMAND_AND_PRELOAD" />;
    host.innerHTML = renderToString(field);
    const selector = within(host).getByRole("combobox", {name: "内容加载"});
    expect(selector).toBeDisabled();
    await act(async () => {root = hydrateRoot(host, field);});
    expect(selector).toBeEnabled();
    await userEvent.setup().selectOptions(selector, "PRELOAD");
    expect(readContentLoading("user-1")).toBe("PRELOAD");
  } finally {
    await act(async () => {root?.unmount();});
    host.remove();
  }
});

it("keeps demand loading available when preference storage is blocked", () => {
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {throw new Error("denied");});
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {throw new Error("denied");});
  expect(() => writeContentLoading("user-1", "PRELOAD")).not.toThrow();
  expect(readContentLoading("user-1")).toBe("ON_DEMAND");
});

it("shows only the supported full-download option without changing the dual-mode preference", async () => {
  writeContentLoading("user-1", "ON_DEMAND");
  render(<ContentLoadingField capability="PRELOAD_ONLY" />);
  const selector = screen.getByRole("combobox", {name: "内容加载"});
  expect(selector).toHaveValue("PRELOAD");
  expect(selector).toBeEnabled();
  expect(within(selector).getAllByRole("option")).toHaveLength(1);
  expect(within(selector).getByRole("option", {name: "下载完成后开始"})).toBeVisible();
  await userEvent.setup().selectOptions(selector, "PRELOAD");
  expect(readContentLoading("user-1")).toBe("ON_DEMAND");
});

it("reserves an inaccessible placeholder when the target does not declare local caching", () => {
  writeContentLoading("user-1", "PRELOAD");
  const {container} = render(<ContentLoadingField />);
  expect(container.querySelector(".is-placeholder")).toHaveAttribute("aria-hidden", "true");
  expect(container.querySelector(".is-placeholder")).toHaveAttribute("inert");
  expect(screen.queryByRole("combobox")).toBeNull();
  expect(readContentLoading("user-1")).toBe("PRELOAD");
});

it("restores the device preference when switching back to a dual-mode target", () => {
  writeContentLoading("user-1", "ON_DEMAND");
  const view = render(<ContentLoadingField capability="ON_DEMAND_AND_PRELOAD" />);
  view.rerender(<ContentLoadingField capability="PRELOAD_ONLY" />);
  expect(screen.getByRole("combobox", {name: "内容加载"})).toHaveValue("PRELOAD");
  view.rerender(<ContentLoadingField />);
  expect(view.container.querySelector(".is-placeholder")).toHaveAttribute("aria-hidden", "true");
  expect(screen.queryByRole("combobox")).toBeNull();
  view.rerender(<ContentLoadingField capability="ON_DEMAND_AND_PRELOAD" />);
  expect(screen.getByRole("combobox", {name: "内容加载"})).toHaveValue("ON_DEMAND");
});

it.each(["ON_DEMAND", "PRELOAD"] as const)("resolves the actual Target with device preference %s", preference => {
  expect(resolveContentLoading("ON_DEMAND_AND_PRELOAD", preference, "PRODUCT")).toBe(preference);
  expect(resolveContentLoading("PRELOAD_ONLY", preference, "PRODUCT")).toBe("PRELOAD");
  expect(resolveContentLoading(undefined, preference, "PRODUCT")).toBeUndefined();
  expect(resolveContentLoading(null, preference, "PRODUCT")).toBeUndefined();
  expect(resolveContentLoading("PRELOAD_ONLY", preference, "REVIEW_PREVIEW")).toBe("ON_DEMAND");
  expect(resolveContentLoading("ON_DEMAND_AND_PRELOAD", preference, "REVIEW_PREVIEW")).toBe("ON_DEMAND");
});
