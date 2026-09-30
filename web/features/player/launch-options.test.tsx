import {act, cleanup, render, screen, within} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {afterEach, beforeEach, expect, it, vi} from "vitest";
import {LaunchControls, type CoreOption, type DOSEntry} from "./launch-controls";
import {readContentLoading} from "./content-loading";

vi.mock("@/features/auth/auth-provider", () => ({useAuth: () => ({context: {user: {userId: "user-1"}}})}));
vi.mock("@/features/mobile/phone-layout", () => ({usePhoneLayout: () => false}));
vi.mock("next/navigation", () => ({useRouter: () => ({replace: vi.fn()})}));

const core: CoreOption = {coreId: "dosbox_pure", name: "DOSBox Pure", isDefault: true, status: "READY", reasons: [], contentLoading: "ON_DEMAND_AND_PRELOAD"};
const entries: DOSEntry[] = [{path: "GAMES/PLAY.EXE", originalPath: "GAMES/PLAY.EXE", kind: "EXE", rank: 0, enabled: true, directLaunchSafe: true}];
let width = 700;
const observers = new Set<() => void>();

beforeEach(() => {
  width = 700;
  localStorage.clear();
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    return {width: this.classList.contains("launch-options") ? width : 0, height: 0, top: 0, bottom: 0, left: 0, right: 0, x: 0, y: 0, toJSON: () => ({})};
  });
  vi.stubGlobal("ResizeObserver", class {
    constructor(callback: () => void) {observers.add(callback);}
    observe() {}
    disconnect() {}
  });
});
afterEach(() => {cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); observers.clear();});

function renderDOS(selected: CoreOption = core, saved?: CoreOption) {
  return render(<LaunchControls gameId="dos" coreOptions={saved ? [selected, saved] : [selected]} dosEntries={entries} defaultDosEntry="GAMES/PLAY.EXE" latestSave={saved ? {saveStateId: "save", sizeBytes: 1, screenshotUrl: null, createdAtMs: 0, coreId: saved.coreId, coreName: saved.name} : undefined} />);
}
function resize(next: number) {act(() => {width = next; observers.forEach(callback => callback());});}

it("puts DOS selectors beside each other when their container is wide enough", () => {
  renderDOS();
  expect(screen.getByLabelText("内容加载").closest(".launch-options")).toHaveClass("is-paired");
  expect(screen.getByLabelText("启动程序")).toBeVisible();
  expect(screen.queryByRole("tablist")).toBeNull();
});

it("switches narrow DOS tabs by click and keyboard, preserving both values across resize", async () => {
  width = 400;
  const user = userEvent.setup();
  renderDOS();
  const loading = screen.getByRole("tab", {name: /内容加载.*按需加载/});
  await user.selectOptions(screen.getByRole("combobox", {name: "内容加载"}), "PRELOAD");
  expect(loading).toHaveAccessibleName(/下载完成后开始/);
  await user.click(screen.getByRole("tab", {name: /启动程序.*PLAY.EXE/}));
  expect(screen.queryByRole("combobox", {name: "内容加载"})).toBeNull();
  await user.selectOptions(screen.getByRole("combobox", {name: "启动程序"}), "");
  expect(screen.getByRole("tab", {name: /启动程序.*程序菜单/})).toHaveAttribute("aria-selected", "true");
  await user.click(screen.getByRole("tab", {name: /启动程序/}));
  await user.keyboard("{ArrowLeft}");
  expect(loading).toHaveFocus();
  expect(screen.getByRole("combobox", {name: "内容加载"})).toHaveValue("PRELOAD");
  await user.keyboard("{End}");
  expect(screen.getByRole("tab", {name: /启动程序/})).toHaveFocus();
  resize(700);
  expect(screen.queryByRole("tablist")).toBeNull();
  expect(screen.getByRole("combobox", {name: "启动程序"})).toHaveValue("");
  expect(screen.getByRole("combobox", {name: "内容加载"})).toHaveValue("PRELOAD");
  resize(400);
  await user.click(screen.getByRole("tab", {name: /内容加载/}));
  expect(screen.getByRole("combobox", {name: "内容加载"})).toHaveValue("PRELOAD");
  expect(readContentLoading("user-1")).toBe("PRELOAD");
  expect(localStorage.getItem("retrom:v2:user:user-1:player:preferred-dos-entry:dos")).toBeNull();
});

it("keeps separate loading capabilities in the loading tab and does not tab a lone DOS field", async () => {
  width = 400;
  const user = userEvent.setup();
  const saved: CoreOption = {...core, coreId: "saved", contentLoading: "PRELOAD_ONLY", isDefault: false};
  const view = renderDOS(core, saved);
  const panel = screen.getByRole("tabpanel", {name: /内容加载/});
  expect(within(panel).getByRole("combobox", {name: "重新开始的内容加载"})).toBeVisible();
  expect(within(panel).getByRole("combobox", {name: "从存档继续的内容加载"})).toHaveValue("PRELOAD");
  await user.click(screen.getByRole("tab", {name: /启动程序/}));
  view.unmount();
  renderDOS({...core, contentLoading: null});
  expect(screen.queryByRole("tablist")).toBeNull();
  expect(screen.getByRole("combobox", {name: "启动程序"})).toBeVisible();
});
