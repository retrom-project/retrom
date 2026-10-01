import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Toast } from "./flash-toast";

describe("Toast", () => {
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("portals a notification outside its card without a close button and dismisses after three seconds", async () => {
    vi.useFakeTimers();
    const onDismiss = vi.fn();
    const { container } = render(<Toast toast={{ message: "保存完成", tone: "good" }} onDismiss={onDismiss} />);

    expect(screen.getByRole("status")).toHaveTextContent("保存完成");
    expect(container).toBeEmptyDOMElement();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    await act(() => vi.advanceTimersByTimeAsync(2_999));
    expect(onDismiss).not.toHaveBeenCalled();
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(onDismiss).toHaveBeenCalledOnce();
  });

  it("does not extend the timeout when a parent changes its dismiss callback", async () => {
    vi.useFakeTimers();
    const toast = { message: "启动失败", tone: "bad" } as const;
    const first = vi.fn(), latest = vi.fn();
    const { rerender } = render(<Toast toast={toast} onDismiss={first} />);
    await act(() => vi.advanceTimersByTimeAsync(2_000));
    rerender(<Toast toast={toast} onDismiss={latest} />);
    await act(() => vi.advanceTimersByTimeAsync(1_000));
    expect(first).not.toHaveBeenCalled();
    expect(latest).toHaveBeenCalledOnce();
  });
});
