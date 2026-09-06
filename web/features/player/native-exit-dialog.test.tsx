import {act, cleanup, fireEvent, render, screen} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {afterEach, expect, it, vi} from "vitest";
import {useNativeExitDecision} from "./native-exit-dialog";

afterEach(() => {cleanup(); vi.useRealTimers(); vi.unstubAllGlobals();});

function fixture(restored = true, dirty = true) {
  const native = {hasChanges: () => dirty, save: vi.fn(async () => true), discard: vi.fn(async () => undefined)};
  const completed = vi.fn();
  function Harness() {
    const exit = useNativeExitDecision(() => restored);
    return <><button onClick={() => {void exit.decide(native).then(completed);}}>请求退出</button>{exit.dialog}</>;
  }
  render(<Harness />);
  return {native, completed, user: userEvent.setup()};
}

it("warns to save inside the game first and lets the user continue without uploading", async () => {
  const f = fixture(); await f.user.click(screen.getByText("请求退出"));
  const dialog = screen.getByRole("alertdialog");
  expect(dialog).toHaveTextContent("请先在游戏内保存");
  expect(dialog).toHaveTextContent("不保存当前画面的即时进度");
  expect(dialog).toHaveTextContent("恢复位置可能与当前画面不同");
  expect(dialog).toHaveTextContent("如果误选了“开始新游戏”");
  expect(screen.getByRole("button", {name: "继续游戏"})).toHaveFocus();
  await f.user.click(screen.getByRole("button", {name: "继续游戏"}));
  expect(f.completed).toHaveBeenCalledWith(false);
  expect(f.native.save).not.toHaveBeenCalled(); expect(f.native.discard).not.toHaveBeenCalled();
});

it("waits for successful save before exiting and retains the dialog on failure", async () => {
  const f = fixture(); f.native.save.mockResolvedValueOnce(false);
  await f.user.click(screen.getByText("请求退出"));
  await f.user.click(screen.getByRole("button", {name: "保存并退出"}));
  expect(screen.getByRole("alert")).toHaveTextContent("草稿已保留");
  expect(f.completed).not.toHaveBeenCalled();
  await f.user.click(screen.getByRole("button", {name: "保存并退出"}));
  expect(f.completed).toHaveBeenCalledWith(true);
});

it("explicit discard exits without saving", async () => {
  const f = fixture(false); await f.user.click(screen.getByText("请求退出"));
  expect(screen.getByRole("alertdialog")).toHaveTextContent("新的独立存档");
  await f.user.click(screen.getByRole("button", {name: "不保存并退出"}));
  expect(f.native.discard).toHaveBeenCalledOnce(); expect(f.native.save).not.toHaveBeenCalled();
  expect(f.completed).toHaveBeenCalledWith(true);
});

it("keeps Enter inside the confirmation instead of the underlying immersive menu", async () => {
  const f = fixture(); await f.user.click(screen.getByText("请求退出"));
  const menu = vi.fn((event: KeyboardEvent) => event.preventDefault());
  window.addEventListener("keydown", menu);
  try {
    await f.user.keyboard("{Enter}");
    expect(f.completed).toHaveBeenCalledWith(false);
    expect(menu).not.toHaveBeenCalled();
  } finally {window.removeEventListener("keydown", menu);}
});

it("skips the save prompt when data matches startup", async () => {
  const f = fixture(false, false); await f.user.click(screen.getByText("请求退出"));
  expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  expect(f.completed).toHaveBeenCalledWith(true);
});

it("locks all actions while saving so discard cannot race a commit", async () => {
  const f = fixture(); let finish!: (saved: boolean) => void;
  f.native.save.mockImplementation(() => new Promise<boolean>((resolve) => {finish = resolve;}));
  await f.user.click(screen.getByText("请求退出"));
  await f.user.click(screen.getByRole("button", {name: "保存并退出"}));
  expect(screen.getByRole("button", {name: "继续游戏"})).toBeDisabled();
  expect(screen.getByRole("button", {name: "保存并退出"})).toBeDisabled();
  await act(async () => {finish(true);});
  expect(f.completed).toHaveBeenCalledOnce();
});

it("moves gamepad focus in visual order and lets B continue without discarding", async () => {
  const f = fixture();
  let pressed = -1;
  vi.stubGlobal("navigator", {getGamepads: () => [{connected: true, mapping: "standard", index: 0, axes: [],
    buttons: Array.from({length: 17}, (_,index) => ({pressed: index === pressed, value: index === pressed ? 1 : 0}))}]});
  vi.useFakeTimers();
  fireEvent.click(screen.getByText("请求退出"));
  await act(async () => {await vi.advanceTimersByTimeAsync(180);});
  pressed = 14; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  expect(screen.getByRole("button", {name: "保存并退出"})).toHaveFocus();
  pressed = -1; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  pressed = 15; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  expect(screen.getByRole("button", {name: "继续游戏"})).toHaveFocus();
  pressed = -1; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  pressed = 15; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  expect(screen.getByRole("button", {name: "不保存并退出"})).toHaveFocus();
  pressed = 1; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  expect(f.completed).toHaveBeenCalledWith(false);
  expect(f.native.save).not.toHaveBeenCalled(); expect(f.native.discard).not.toHaveBeenCalled();
});
