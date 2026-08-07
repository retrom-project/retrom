import { cleanup, render, screen } from "@testing-library/react";
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
});
