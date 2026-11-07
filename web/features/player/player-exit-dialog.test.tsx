import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { PlayerExitDialog } from "./player-exit-dialog";
afterEach(cleanup);
function controller() {
  return { open: true, active: true, opening: false, busy: false, saveState: "idle" as const, request: vi.fn(async () => {}), cancel: vi.fn(async () => {}), save: vi.fn(async () => {}), confirm: vi.fn(async () => {}), dismiss: vi.fn(() => true) };
}
it.each([false, true])("native menu exposes data sync instead of instant capture (immersive=%s)", (immersive) => {
  const controls = controller();
  render(<PlayerExitDialog immersive={immersive} native available draft={false} controller={controls} />);
  expect(screen.queryByRole("button", { name: "创建存档" })).not.toBeInTheDocument();
  expect(screen.getByText(/不保存当前画面的即时进度/u)).toBeVisible();
  const sync = screen.getByRole("button", { name: "同步存档" });
  expect(sync).toBeEnabled();
  fireEvent.click(sync);
  expect(controls.save).toHaveBeenCalledOnce();
  expect(controls.confirm).not.toHaveBeenCalled();
});
it("offers retry for the durable native draft even when Runtime has no new export", () => {
  const controls = controller();
  render(<PlayerExitDialog immersive={false} native available={false} draft controller={controls} />);
  const retry = screen.getByRole("button", { name: "重试同步存档" });
  expect(retry).toBeEnabled();
  fireEvent.click(retry);
  expect(controls.save).toHaveBeenCalledOnce();
});
it.each([false, true])("a game that ended itself offers a restart instead of a save action (immersive=%s)", (immersive) => {
  const controls = controller();
  const restart = vi.fn();
  render(<PlayerExitDialog immersive={immersive} native available={false} draft={false} controller={controls} onRestart={restart} />);
  expect(screen.queryByRole("button", { name: "同步存档" })).not.toBeInTheDocument();
  expect(screen.getByText(/游戏已自行结束/u)).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "重新启动游戏" }));
  expect(restart).toHaveBeenCalledOnce();
  expect(controls.save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "退出游戏" }));
  expect(controls.confirm).toHaveBeenCalledOnce();
});
