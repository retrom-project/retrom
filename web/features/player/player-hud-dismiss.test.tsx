import {cleanup, fireEvent, render, screen} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import {PlayerHudDismiss} from "./player-hud-dismiss";

afterEach(cleanup);
it("dismisses only on a surface click, never because a game takes focus", () => {
  const dismiss = vi.fn();
  render(<PlayerHudDismiss visible enabled state="RUNNING" immersive={false} onDismiss={dismiss} />);
  fireEvent.blur(window);
  fireEvent.focus(window);
  expect(dismiss).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", {name: "收起游戏工具栏"}));
  expect(dismiss).toHaveBeenCalledOnce();
});
it("leaves immersive play, dialogs, paused overlays and hidden HUD game input alone", () => {
  const props = {visible: true, enabled: true, state: "RUNNING" as const, immersive: false, onDismiss: vi.fn()};
  const view = render(<PlayerHudDismiss {...props} immersive />);
  expect(screen.queryByRole("button")).toBeNull();
  view.rerender(<PlayerHudDismiss {...props} enabled={false} />);
  expect(screen.queryByRole("button")).toBeNull();
  view.rerender(<PlayerHudDismiss {...props} state="PAUSED" />);
  expect(screen.queryByRole("button")).toBeNull();
  view.rerender(<PlayerHudDismiss {...props} visible={false} />);
  expect(screen.queryByRole("button")).toBeNull();
});
