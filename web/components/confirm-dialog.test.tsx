import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ConfirmDialog } from "./confirm-dialog";

afterEach(cleanup);

describe("ConfirmDialog", () => {
  it("focuses the safe action, traps keyboard focus, and cancels with Escape", async () => {
    const user = userEvent.setup();
    const cancel = vi.fn();
    render(<ConfirmDialog open title="确认更改？" secondaryLabel="稍后处理" onCancel={cancel} onSecondary={() => undefined} onConfirm={() => undefined}>影响摘要</ConfirmDialog>);

    expect(screen.getByRole("button", { name: "取消" })).toHaveFocus();
    await user.tab({ shift: true });
    expect(screen.getByRole("button", { name: "确认" })).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(cancel).toHaveBeenCalledOnce();
  });

  it("renders a leading action and locks every decision while it is busy", async () => {
    const user = userEvent.setup();
    const leading = vi.fn();
    const cancel = vi.fn();
    render(
      <ConfirmDialog
        open
        title="退出游戏？"
        leadingLabel="创建存档"
        leadingBusy
        leadingBusyLabel="正在创建…"
        onLeading={leading}
        onCancel={cancel}
        onConfirm={() => undefined}
      />,
    );

    const buttons = screen.getAllByRole("button");
    expect(buttons.map((button) => button.textContent)).toEqual(["正在创建…", "取消", "确认"]);
    expect(buttons.every((button) => button.hasAttribute("disabled"))).toBe(true);
    await user.keyboard("{Escape}");
    expect(cancel).not.toHaveBeenCalled();
    expect(leading).not.toHaveBeenCalled();
  });

  it("can portal the viewport layer out of a transformed card", () => {
    render(<article data-testid="card" style={{ transform: "translateY(-3px)" }}>
      <ConfirmDialog open portalToBody title="取消收藏？" onCancel={() => undefined} onConfirm={() => undefined} />
    </article>);

    const card = screen.getByTestId("card");
    const dialog = screen.getByRole("alertdialog", { name: "取消收藏？" });
    expect(within(card).queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(dialog.parentElement).toHaveClass("dialog-backdrop");
    expect(dialog.parentElement?.parentElement).toBe(document.body);
  });
});
