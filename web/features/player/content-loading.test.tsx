import {afterEach, expect, it, vi} from "vitest";
import {cleanup, render, screen} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {ContentLoadingField} from "./content-loading-field";
import {readContentLoading, writeContentLoading} from "./content-loading";

vi.mock("@/features/auth/auth-provider", () => ({useAuth: () => ({context: {user: {userId: "user-1"}}})}));
afterEach(() => {cleanup(); localStorage.clear(); vi.restoreAllMocks();});

it("defaults to demand loading and remembers a device preference for the current user", async () => {
  render(<ContentLoadingField />);
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
