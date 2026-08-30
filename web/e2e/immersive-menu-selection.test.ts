import { describe, expect, it, vi } from "vitest";
import { selectImmersiveMenuItem } from "./immersive-menu-selection";

describe("immersive acceptance menu selection", () => {
  it("observes the current item and retries a dropped gamepad direction", async () => {
    let current = "取消";
    const press = vi.fn(async () => {
      if (press.mock.calls.length === 2) {current = "创建存档";}
    });

    await selectImmersiveMenuItem("创建存档", async () => current, press);

    expect(press).toHaveBeenCalledTimes(2);
    expect(press).toHaveBeenNthCalledWith(1, "right");
    expect(press).toHaveBeenNthCalledWith(2, "right");
  });

  it("fails in a bounded way when the current selection is invalid", async () => {
    await expect(selectImmersiveMenuItem("退出游戏", async () => null, vi.fn()))
      .rejects.toThrow("IMMERSIVE_MENU_SELECTION_INVALID:none");
  });
});
