import {afterEach, expect, it, vi} from "vitest";
import {cleanup, render, screen} from "@testing-library/react";
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

it("keeps demand loading available when preference storage is blocked", () => {
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {throw new Error("denied");});
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {throw new Error("denied");});
  expect(() => writeContentLoading("user-1", "PRELOAD")).not.toThrow();
  expect(readContentLoading("user-1")).toBe("ON_DEMAND");
});

it("shows a fixed full download indication without changing the dual-mode preference", () => {
  writeContentLoading("user-1", "ON_DEMAND");
  render(<ContentLoadingField capability="PRELOAD_ONLY" />);
  expect(screen.queryByRole("combobox")).toBeNull();
  expect(screen.getByText("下载完成后开始")).toBeVisible();
  expect(readContentLoading("user-1")).toBe("ON_DEMAND");
});

it("hides loading controls when the target does not declare local caching", () => {
  writeContentLoading("user-1", "PRELOAD");
  const {container} = render(<ContentLoadingField />);
  expect(container).toBeEmptyDOMElement();
  expect(readContentLoading("user-1")).toBe("PRELOAD");
});

it("restores the device preference when switching back to a dual-mode target", () => {
  writeContentLoading("user-1", "PRELOAD");
  const view = render(<ContentLoadingField capability="ON_DEMAND_AND_PRELOAD" />);
  view.rerender(<ContentLoadingField capability="PRELOAD_ONLY" />);
  expect(screen.queryByRole("combobox")).toBeNull();
  view.rerender(<ContentLoadingField />);
  expect(view.container).toBeEmptyDOMElement();
  view.rerender(<ContentLoadingField capability="ON_DEMAND_AND_PRELOAD" />);
  expect(screen.getByRole("combobox", {name: "内容加载"})).toHaveValue("PRELOAD");
});

it.each(["ON_DEMAND", "PRELOAD"] as const)("resolves the actual Target with device preference %s", preference => {
  expect(resolveContentLoading("ON_DEMAND_AND_PRELOAD", preference, "PRODUCT")).toBe(preference);
  expect(resolveContentLoading("PRELOAD_ONLY", preference, "PRODUCT")).toBe("PRELOAD");
  expect(resolveContentLoading(undefined, preference, "PRODUCT")).toBeUndefined();
  expect(resolveContentLoading(null, preference, "PRODUCT")).toBeUndefined();
  expect(resolveContentLoading("PRELOAD_ONLY", preference, "REVIEW_PREVIEW")).toBe("ON_DEMAND");
  expect(resolveContentLoading("ON_DEMAND_AND_PRELOAD", preference, "REVIEW_PREVIEW")).toBe("ON_DEMAND");
});
