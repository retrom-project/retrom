import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Toast } from "./flash-toast";

describe("Toast", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("dismisses a visible notification after two seconds", async () => {
    vi.useFakeTimers();
    const onDismiss = vi.fn();
    render(<Toast toast={{ message: "保存完成", tone: "good" }} onDismiss={onDismiss} />);

    expect(screen.getByRole("status")).toHaveTextContent("保存完成");
    await act(() => vi.advanceTimersByTimeAsync(1_999));
    expect(onDismiss).not.toHaveBeenCalled();
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(onDismiss).toHaveBeenCalledOnce();
  });
});
