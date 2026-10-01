import { ToastProvider } from "@/components/toast-provider";
import {afterEach, expect, it, vi} from "vitest";
import {cleanup, render, screen} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {LaunchControls, type CoreOption} from "./launch-controls";
import {readContentLoading, writeContentLoading} from "./content-loading";

const layout = vi.hoisted(() => ({phone: false}));
vi.mock("@/features/mobile/phone-layout", () => ({usePhoneLayout: () => layout.phone}));
vi.mock("@/features/auth/auth-provider", () => ({useAuth: () => ({context: {user: {userId: "user-1"}}})}));
vi.mock("next/navigation", () => ({useRouter: () => ({replace: vi.fn()})}));
const cores: CoreOption[] = [
  {coreId: "dual", name: "双模式", isDefault: true, status: "READY", reasons: [], contentLoading: "ON_DEMAND_AND_PRELOAD"},
  {coreId: "full", name: "整文件", isDefault: false, status: "READY", reasons: [], contentLoading: "PRELOAD_ONLY"},
  {coreId: "upstream", name: "原加载器", isDefault: false, status: "READY", reasons: []},
];
afterEach(() => {cleanup(); localStorage.clear(); layout.phone = false;});

it.each([false, true])("updates loading controls when the selected core changes (phone=%s)", async phone => {
  layout.phone = phone;
  const user = userEvent.setup();
  writeContentLoading("user-1", "PRELOAD");
  render(<LaunchControls gameId="game" coreOptions={cores} dosEntries={[]} defaultDosEntry={null} />, { wrapper: ToastProvider });
  if (phone) {await user.click(screen.getByRole("button", {name: "启动选项"}));}
  const select = async (core: string) => {
    if (!phone) {await user.click(screen.getByRole("button", {name: "更换"}));}
    await user.selectOptions(screen.getByRole("combobox", {name: phone ? "运行方式" : "运行引擎"}), core);
    if (!phone) {await user.click(screen.getByRole("button", {name: "应用"}));}
  };
  expect(screen.getByRole("combobox", {name: "内容加载"})).toHaveValue("PRELOAD");
  await select("full");
  expect(screen.getByRole("combobox", {name: "内容加载"})).toHaveValue("PRELOAD");
  expect(screen.getByRole("combobox", {name: "内容加载"}).querySelectorAll("option")).toHaveLength(1);
  await select("upstream");
  expect(screen.queryByRole("combobox", {name: "内容加载"})).toBeNull();
  expect(document.querySelector(".content-loading-field.is-placeholder")).toHaveAttribute("aria-hidden", "true");
  expect(document.querySelector(".content-loading-field.is-placeholder")).toHaveAttribute("inert");
  expect(screen.queryByRole("option", {name: "下载完成后开始"})).toBeNull();
  await select("dual");
  expect(screen.getByRole("combobox", {name: "内容加载"})).toHaveValue("PRELOAD");
  expect(readContentLoading("user-1")).toBe("PRELOAD");
});

it.each([false, true])("shows the saved core capability independently of the new-game core (phone=%s)", async phone => {
  layout.phone = phone;
  const user = userEvent.setup();
  const latestSave = {saveStateId: "save", sizeBytes: 3, screenshotUrl: null, createdAtMs: 0, coreId: "full", coreName: "整文件"};
  render(<LaunchControls gameId="game" coreOptions={cores} dosEntries={[]} defaultDosEntry={null} latestSave={latestSave} />, { wrapper: ToastProvider });
  if (phone) {await user.click(screen.getByRole("button", {name: "启动选项"}));}
  expect(screen.getByRole("combobox", {name: "重新开始的内容加载"})).toBeVisible();
  expect(screen.getByRole("combobox", {name: "从存档继续的内容加载"})).toHaveValue("PRELOAD");
});

it("does not describe an unresolved saved core using the new-game capability", () => {
  const latestSave = {saveStateId: "save", sizeBytes: 3, screenshotUrl: null, createdAtMs: 0, coreId: "unavailable", coreName: "Unavailable"};
  render(<LaunchControls gameId="game" coreOptions={cores} dosEntries={[]} defaultDosEntry={null} latestSave={latestSave} />, { wrapper: ToastProvider });
  expect(screen.getByRole("combobox", {name: "重新开始的内容加载"})).toBeVisible();
  expect(screen.queryByRole("combobox", {name: "内容加载"})).toBeNull();
});
