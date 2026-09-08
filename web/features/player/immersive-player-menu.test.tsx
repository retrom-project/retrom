import { fireEvent, render, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ImmersivePlayerMenu } from "./immersive-player-menu";

describe("ImmersivePlayerMenu", () => {
  it("offers native capture as the controller's save action when the game supports it", () => {
    const onConfirm = vi.fn();
    const view = render(<ImmersivePlayerMenu checkpointSemantics="GAME_SAVE" saveAvailable
      nativeSave={{capture: "RUNTIME", restore: "AUTOMATIC", captureAvailable: true}}
      overlay={{kind: "menu", error: "", notice: "", pending: false, selected: 1}}
      onCancel={vi.fn()} onConfirm={onConfirm} onSelect={vi.fn()} />);
    const content = within(view.container);
    const button = content.getByRole("button", {name: "创建存档"});
    expect(button).toBeEnabled(); expect(button).toHaveAttribute("aria-current", "true");
    fireEvent.click(button); expect(onConfirm).toHaveBeenCalledOnce();
    expect(content.queryByRole("button", {name: "重试暂存"})).toBeNull();
    expect(content.getByRole("dialog")).toHaveTextContent("会自动恢复到其记录的位置");
  });
  it("exposes cancel, save, and exit with stable accessible names", () => {
    const onCancel = vi.fn();
    const onConfirm = vi.fn();
    const onSelect = vi.fn();
    const view = render(<ImmersivePlayerMenu
      overlay={{ kind: "menu", error: "", notice: "", pending: false, selected: 0 }}
      saveAvailable
      onCancel={onCancel}
      onConfirm={onConfirm}
      onSelect={onSelect}
    />);
    const content = within(view.container);
    expect(content.getByRole("dialog", { name: "游戏菜单" })).toBeTruthy();
    fireEvent.click(content.getByRole("button", { name: "取消" }));
    fireEvent.click(content.getByRole("button", { name: "创建存档" }));
    fireEvent.click(content.getByRole("button", { name: "退出游戏" }));
    expect(onCancel).toHaveBeenCalledOnce();
    expect(onSelect).toHaveBeenCalledWith(1);
    expect(onSelect).toHaveBeenCalledWith(2);
    expect(onConfirm).toHaveBeenCalledTimes(2);
  });

  it("disables save with an explicit reason when the runtime is incompatible", () => {
    const callbacks = { onCancel: vi.fn(), onConfirm: vi.fn(), onSelect: vi.fn() };
    const view = render(<ImmersivePlayerMenu
      overlay={{ kind: "menu", error: "", notice: "", pending: false, selected: 0 }}
      saveAvailable={false}
      {...callbacks}
    />);
    const content = within(view.container);
    expect(content.getByRole("button", { name: "创建存档" })).toBeDisabled();
    expect(content.getByText("当前运行方式无法创建可恢复存档。")).toBeTruthy();
  });

  it("keeps reconnect and neutral-wait states modal without actions", () => {
    const callbacks = { onCancel: vi.fn(), onConfirm: vi.fn(), onSelect: vi.fn() };
    const view = render(<ImmersivePlayerMenu overlay={{ kind: "reconnect", ready: false }} saveAvailable {...callbacks} />);
    const content = within(view.container);
    expect(content.getByRole("alertdialog", { name: "请重新连接手柄" })).toBeTruthy();
    expect(content.queryByRole("button")).toBeNull();
    view.rerender(<ImmersivePlayerMenu overlay={{ kind: "closing" }} saveAvailable {...callbacks} />);
    expect(content.getByText("请松开手柄按键…")).toBeTruthy();
    view.rerender(<ImmersivePlayerMenu overlay={{ kind: "closed" }} saveAvailable {...callbacks} />);
    expect(content.queryByText("请松开手柄按键…")).toBeNull();
  });
});

it("explains native save semantics in the controller menu", () => {
  const view = render(<ImmersivePlayerMenu checkpointSemantics="GAME_SAVE" saveAvailable
    overlay={{kind: "menu", error: "", notice: "", pending: false, selected: 0}}
    onCancel={vi.fn()} onConfirm={vi.fn()} onSelect={vi.fn()} />);
  expect(within(view.container).getByRole("dialog")).toHaveTextContent("请先在游戏内保存");
  expect(within(view.container).getByRole("dialog")).toHaveTextContent("恢复后请从游戏菜单读档");
});

it("disables unchanged native saves while explaining local drafts", () => {
  const view = render(<ImmersivePlayerMenu checkpointSemantics="GAME_SAVE" saveAvailable={false} saveStatus="原生存档已同步"
    overlay={{kind: "menu", error: "", notice: "", pending: false, selected: 0}}
    onCancel={vi.fn()} onConfirm={vi.fn()} onSelect={vi.fn()} />);
  expect(within(view.container).getByRole("button", {name: "创建存档"})).toBeDisabled();
  expect(within(view.container).getByText("原生存档已同步")).toBeVisible();
  expect(within(view.container).getByRole("dialog")).toHaveTextContent("再在退出时选择“存档并退出”");
});
