import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MultiDiscModeField } from "./multidisc-mode-field";

afterEach(cleanup);

describe("MultiDiscModeField", () => {
  it("uses a compact checkbox without inheriting full-width field sizing", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<MultiDiscModeField selected detectedGroupCount={1} maxDiscs={8} maxTotalBytes={1024} onChange={onChange} />);

    const checkbox = screen.getByRole("checkbox", { name: /多盘游戏/ });
    expect(checkbox).toHaveClass("multi-disc-mode-checkbox");
    expect(checkbox).toBeChecked();
    await user.click(checkbox);
    expect(onChange).toHaveBeenCalledWith(false);
  });
});
