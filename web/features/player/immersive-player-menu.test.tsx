import { fireEvent, render, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ImmersivePlayerMenu } from "./immersive-player-menu";

describe("ImmersivePlayerMenu", () => {
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
