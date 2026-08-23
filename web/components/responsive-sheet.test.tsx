import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createRef } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ResponsiveSheet } from "./responsive-sheet";

afterEach(() => {
  cleanup();
  document.body.style.overflow = "";
  document.body.style.paddingRight = "";
});

describe("ResponsiveSheet", () => {
  it("locks background scrolling, traps focus, and restores the trigger", async () => {
    const user = userEvent.setup();
    const trigger = createRef<HTMLButtonElement>();
    const close = vi.fn();
    const { rerender } = render(<>
      <button ref={trigger}>打开筛选</button>
      <ResponsiveSheet open title="筛选" onClose={close} returnFocusRef={trigger}>
        <button>第一项</button><button>最后一项</button>
      </ResponsiveSheet>
    </>);

    await new Promise((resolve) => requestAnimationFrame(resolve));
    expect(document.body.style.overflow).toBe("hidden");
    expect(screen.getAllByRole("button", { name: "关闭筛选" }).at(-1)).toHaveFocus();
    await user.tab({ shift: true });
    expect(screen.getByRole("button", { name: "最后一项" })).toHaveFocus();

    rerender(<>
      <button ref={trigger}>打开筛选</button>
      <ResponsiveSheet open={false} title="筛选" onClose={close} returnFocusRef={trigger}><button>第一项</button></ResponsiveSheet>
    </>);
    expect(document.body.style.overflow).toBe("");
    expect(trigger.current).toHaveFocus();
  });

  it("closes on Escape and from the visible close action", async () => {
    const user = userEvent.setup();
    const close = vi.fn();
    render(<ResponsiveSheet open title="筛选" onClose={close}><button>字段</button></ResponsiveSheet>);
    await new Promise((resolve) => requestAnimationFrame(resolve));
    await user.keyboard("{Escape}");
    expect(close).toHaveBeenCalledTimes(1);
    const closeActions = screen.getAllByRole("button", { name: "关闭筛选" });
    await user.click(closeActions.at(-1)!);
    expect(close).toHaveBeenCalledTimes(2);
  });

  it("advances through body and footer controls without relying on viewport tab order", async () => {
    const user = userEvent.setup();
    const input = createRef<HTMLInputElement>();
    render(
      <ResponsiveSheet
        open
        title="新建标签"
        onClose={() => undefined}
        initialFocusRef={input}
        footer={<><button disabled>不可用操作</button><button>取消</button><button>保存标签</button></>}
      >
        <input ref={input} aria-label="标签名称" />
      </ResponsiveSheet>,
    );
    await new Promise((resolve) => requestAnimationFrame(resolve));

    expect(screen.getByRole("textbox", { name: "标签名称" })).toHaveFocus();
    await user.tab();
    expect(screen.getByRole("button", { name: "取消" })).toHaveFocus();
    await user.tab();
    expect(screen.getByRole("button", { name: "保存标签" })).toHaveFocus();
  });

  it("does not steal focus already moved inside the sheet before initial focus runs", async () => {
    const input = createRef<HTMLInputElement>();
    render(
      <ResponsiveSheet open title="新建标签" onClose={() => undefined} initialFocusRef={input}
        footer={<button>取消</button>}>
        <input ref={input} aria-label="标签名称" />
      </ResponsiveSheet>,
    );
    const cancel = screen.getByRole("button", { name: "取消" });
    cancel.focus();
    await new Promise((resolve) => requestAnimationFrame(resolve));
    expect(cancel).toHaveFocus();
  });
});
