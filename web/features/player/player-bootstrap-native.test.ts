import { describe, expect, it, vi } from "vitest";
import { schedulePlayerCanvasRefresh } from "./player-bootstrap";

describe("native RPG Maker frame refresh", () => {
  it("never reads requestAnimationFrame from the unique-origin runtime Window", () => {
    const refresh = vi.fn();
    const parentFrame = vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      callback(0);
      return 1;
    });
    const uniqueOriginWindow = {
      get requestAnimationFrame(): never {
        throw new DOMException("Blocked a frame with origin", "SecurityError");
      },
    } as unknown as Window;

    expect(() => schedulePlayerCanvasRefresh(true, uniqueOriginWindow, refresh)).not.toThrow();
    expect(parentFrame).toHaveBeenCalledOnce();
    expect(refresh).toHaveBeenCalledOnce();
  });
});
