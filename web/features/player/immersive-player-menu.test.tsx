import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ImmersivePlayerMenu } from "./immersive-player-menu";

describe("ImmersivePlayerMenu", () => {
  it("exposes only cancel and exit with stable accessible names", () => {
    const onCancel = vi.fn();
    const onConfirm = vi.fn();
    const onSelect = vi.fn();
    render(<ImmersivePlayerMenu
      overlay={{ kind: "menu", error: "", pending: false, selected: 0 }}
      onCancel={onCancel}
      onConfirm={onConfirm}
      onSelect={onSelect}
    />);
    expect(screen.getByRole("dialog", { name: "游戏菜单" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    fireEvent.focus(screen.getByRole("button", { name: "退出游戏" }));
    fireEvent.click(screen.getByRole("button", { name: "退出游戏" }));
    expect(onCancel).toHaveBeenCalledOnce();
    expect(onSelect).toHaveBeenCalledWith(1);
    expect(onConfirm).toHaveBeenCalledOnce();
    expect(screen.queryByRole("button", { name: "存档" })).toBeNull();
  });

  it("keeps reconnect and neutral-wait states modal without actions", () => {
    const callbacks = { onCancel: vi.fn(), onConfirm: vi.fn(), onSelect: vi.fn() };
    const view = render(<ImmersivePlayerMenu overlay={{ kind: "reconnect", ready: false }} {...callbacks} />);
    const content = within(view.container);
    expect(content.getByRole("alertdialog", { name: "请重新连接手柄" })).toBeTruthy();
    expect(content.queryByRole("button")).toBeNull();
    view.rerender(<ImmersivePlayerMenu overlay={{ kind: "closing" }} {...callbacks} />);
    expect(content.getByText("请松开手柄按键…")).toBeTruthy();
    view.rerender(<ImmersivePlayerMenu overlay={{ kind: "closed" }} {...callbacks} />);
    expect(content.queryByText("请松开手柄按键…")).toBeNull();
  });
});
