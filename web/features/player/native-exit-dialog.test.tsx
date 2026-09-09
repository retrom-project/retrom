import {act, cleanup, fireEvent, render, screen} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {afterEach, expect, it, vi} from "vitest";
import {useNativeExitDecision} from "./native-exit-dialog";

afterEach(() => {cleanup(); vi.useRealTimers(); vi.unstubAllGlobals();});

function fixture(restored = true, dirty = true, canResume = true, canCapture = false, storage = false) {
  const native = {isStorageContainer: () => storage, canCapture: () => canCapture, capture: vi.fn(async () => true), retry: vi.fn(async () => true), hasChanges: () => dirty, save: vi.fn(async () => true), discard: vi.fn(async () => undefined)};
  const completed = vi.fn();
  function Harness() {
    const exit = useNativeExitDecision(() => restored);
    return <><button onClick={() => {void exit.decide(native, {canResume}).then(completed);}}>请求退出</button>{exit.dialog}</>;
  }
  render(<Harness />);
  return {native, completed, user: userEvent.setup()};
}

it("warns about overwriting changed data and defaults to returning to the game without uploading", async () => {
  const f = fixture(); await f.user.click(screen.getByText("请求退出"));
  const dialog = screen.getByRole("alertdialog");
  expect(dialog).toHaveTextContent("当前存档数据已发生变更，是否保存存档？");
  expect(dialog).toHaveTextContent("主动执行过“保存游戏”");
  expect(dialog).toHaveTextContent("避免异常数据变更覆盖此前的存档");
  expect(dialog).toHaveTextContent("不保存当前画面的即时进度");
  expect(dialog).toHaveTextContent("保存将更新启动时选择的存档");
  expect(Array.from(dialog.querySelectorAll("button"), (button) => button.textContent))
    .toEqual(["返回游戏", "直接退出", "存档并退出"]);
  expect(screen.getByRole("button", {name: "返回游戏"})).toHaveFocus();
  await f.user.click(screen.getByRole("button", {name: "返回游戏"}));
  expect(f.completed).toHaveBeenCalledWith(false);
  expect(f.native.save).not.toHaveBeenCalled(); expect(f.native.discard).not.toHaveBeenCalled();
});

it("keeps storage containers writable without promising progress recovery", async () => {
  const f = fixture(true, true, true, false, true);
  await f.user.click(screen.getByText("请求退出"));
  expect(screen.getByRole("alertdialog")).toHaveTextContent("后续保存会更新同一个存档");
  expect(screen.getByRole("alertdialog")).toHaveTextContent("不保证恢复关卡进度");
  const save = screen.getByRole("button", {name: "存档并退出"});
  expect(save).toBeEnabled();
  await f.user.click(save);
  expect(f.native.save).toHaveBeenCalledOnce();
  expect(f.completed).toHaveBeenCalledWith(true);
});

it("waits for successful save before exiting and retains the dialog on failure", async () => {
  const f = fixture(); f.native.save.mockResolvedValueOnce(false);
  await f.user.click(screen.getByText("请求退出"));
  await f.user.click(screen.getByRole("button", {name: "存档并退出"}));
  expect(screen.getByRole("alert")).toHaveTextContent("草稿已保留");
  expect(f.completed).not.toHaveBeenCalled();
  await f.user.click(screen.getByRole("button", {name: "存档并退出"}));
  expect(f.completed).toHaveBeenCalledWith(true);
});

it("explicit discard exits without saving", async () => {
  const f = fixture(false); await f.user.click(screen.getByText("请求退出"));
  expect(screen.getByRole("alertdialog")).toHaveTextContent("新的独立存档");
  await f.user.click(screen.getByRole("button", {name: "直接退出"}));
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

it.each([false, true])("asks before exiting unchanged data with restored=%s and defaults to returning", async (restored) => {
  const f = fixture(restored, false); await f.user.click(screen.getByText("请求退出"));
  const dialog = screen.getByRole("alertdialog");
  expect(dialog).toHaveTextContent("此次游玩似乎没有进行过存档操作，是否继续退出？");
  expect(dialog).toHaveTextContent("为避免游戏进度丢失，请确保在退出前已在游戏中主动保存");
  expect(Array.from(dialog.querySelectorAll("button"), (button) => button.textContent))
    .toEqual(["返回游戏", "继续退出"]);
  expect(screen.getByRole("button", {name: "返回游戏"})).toHaveFocus();
  expect(f.completed).not.toHaveBeenCalled();
  await f.user.keyboard("{Enter}");
  expect(f.completed).toHaveBeenCalledWith(false);
  expect(f.native.save).not.toHaveBeenCalled(); expect(f.native.discard).not.toHaveBeenCalled();
});

it("only exits unchanged data after explicit confirmation, without saving", async () => {
  const f = fixture(false, false); await f.user.click(screen.getByText("请求退出"));
  await f.user.click(screen.getByRole("button", {name: "继续退出"}));
  expect(f.completed).toHaveBeenCalledWith(true);
  expect(f.native.save).not.toHaveBeenCalled();
  expect(f.native.discard).not.toHaveBeenCalled();
});

it("locks all actions while saving so discard cannot race a commit", async () => {
  const f = fixture(); let finish!: (saved: boolean) => void;
  f.native.save.mockImplementation(() => new Promise<boolean>((resolve) => {finish = resolve;}));
  await f.user.click(screen.getByText("请求退出"));
  await f.user.click(screen.getByRole("button", {name: "存档并退出"}));
  expect(screen.getByRole("button", {name: "返回游戏"})).toBeDisabled();
  expect(screen.getByRole("button", {name: "直接退出"})).toBeDisabled();
  expect(screen.getByRole("button", {name: "处理中…"})).toBeDisabled();
  expect(f.completed).not.toHaveBeenCalled();
  await f.user.keyboard("{Escape}");
  expect(f.completed).not.toHaveBeenCalled();
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
  expect(screen.getByRole("button", {name: "存档并退出"})).toHaveFocus();
  pressed = -1; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  pressed = 15; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  expect(screen.getByRole("button", {name: "返回游戏"})).toHaveFocus();
  pressed = -1; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  pressed = 15; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  expect(screen.getByRole("button", {name: "直接退出"})).toHaveFocus();
  pressed = 1; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  expect(f.completed).toHaveBeenCalledWith(false);
  expect(f.native.save).not.toHaveBeenCalled(); expect(f.native.discard).not.toHaveBeenCalled();
});

it("cycles only the two visible actions for unchanged data and confirms exit with gamepad A", async () => {
  const f = fixture(false, false);
  let pressed = -1;
  vi.stubGlobal("navigator", {getGamepads: () => [{connected: true, mapping: "standard", index: 0, axes: [],
    buttons: Array.from({length: 17}, (_, index) => ({pressed: index === pressed, value: index === pressed ? 1 : 0}))}]});
  vi.useFakeTimers();
  fireEvent.click(screen.getByText("请求退出"));
  await act(async () => {await vi.advanceTimersByTimeAsync(180);});
  pressed = 14; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  expect(screen.getByRole("button", {name: "继续退出"})).toHaveFocus();
  pressed = -1; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  pressed = 14; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  expect(screen.getByRole("button", {name: "返回游戏"})).toHaveFocus();
  pressed = -1; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  pressed = 15; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  expect(screen.getByRole("button", {name: "继续退出"})).toHaveFocus();
  pressed = 0; await act(async () => {await vi.advanceTimersByTimeAsync(32);});
  expect(f.completed).toHaveBeenCalledWith(true);
  expect(f.native.save).not.toHaveBeenCalled();
});

it.each([true, false])("Escape returns to the game without saving or discarding when dirty=%s", async (dirty) => {
  const f = fixture(true, dirty); await f.user.click(screen.getByText("请求退出"));
  await f.user.keyboard("{Escape}");
  expect(f.completed).toHaveBeenCalledWith(false);
  expect(f.native.save).not.toHaveBeenCalled(); expect(f.native.discard).not.toHaveBeenCalled();
});

it("retains the dialog and local data when discarding fails, then allows retry", async () => {
  const f = fixture(); f.native.discard.mockRejectedValueOnce(Error("storage unavailable"));
  await f.user.click(screen.getByText("请求退出"));
  await f.user.click(screen.getByRole("button", {name: "直接退出"}));
  expect(screen.getByRole("alert")).toHaveTextContent("本地数据已保留");
  expect(f.completed).not.toHaveBeenCalled();
  await f.user.click(screen.getByRole("button", {name: "直接退出"}));
  expect(f.completed).toHaveBeenCalledWith(true);
  expect(f.native.save).not.toHaveBeenCalled();
});


it("keeps final files on cancel after the engine has ended instead of offering to return to a closed game", async () => {
  const f = fixture(true, true, false); await f.user.click(screen.getByText("请求退出"));
  expect(screen.queryByRole("button", {name: "返回游戏"})).not.toBeInTheDocument();
  expect(screen.getByRole("button", {name: "保留草稿并退出"})).toHaveFocus();
  await f.user.keyboard("{Escape}");
  expect(f.completed).toHaveBeenCalledWith(true);
  expect(f.native.save).not.toHaveBeenCalled(); expect(f.native.discard).not.toHaveBeenCalled();
});


it("retains the final-exit dialog if the local draft cannot be persisted", async () => {
  const f = fixture(true, true, false); f.native.retry.mockResolvedValueOnce(false);
  await f.user.click(screen.getByText("请求退出")); await f.user.click(screen.getByRole("button", {name: "保留草稿并退出"}));
  expect(f.completed).not.toHaveBeenCalled(); expect(screen.getByRole("alert")).toHaveTextContent("草稿尚未存入此浏览器");
  await f.user.click(screen.getByRole("button", {name: "存档并退出"})); expect(f.completed).toHaveBeenCalledWith(true);
});

it("offers native capture on exit even before any save file exists", async () => {
  const f = fixture(false, false, true, true);
  await f.user.click(screen.getByText("请求退出"));
  expect(screen.getByRole("button", {name: "返回游戏"})).toHaveFocus();
  expect(screen.getByRole("alertdialog")).toHaveTextContent("游戏自身的存档功能");
  await f.user.click(screen.getByRole("button", {name: "存档并退出"}));
  expect(f.native.capture).toHaveBeenCalledOnce();
  expect(f.native.save).not.toHaveBeenCalled();
  expect(f.completed).toHaveBeenCalledWith(true);
});
