import {act, cleanup, fireEvent, render, screen} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {afterEach, expect, it, vi} from "vitest";
import {PlayerEmulatorSettings} from "./player-emulator-settings";

afterEach(() => {cleanup(); vi.restoreAllMocks();});

function props() {
  return {mobile: true, capabilities: {nativeSettings: true, volume: true, standardGamepad: true, videoModes: ["pixel" as const]},
    volume: 0.5, muted: false, renderingMode: "pixel" as const, onHold: vi.fn(), onMute: vi.fn(), onVolume: vi.fn(), onRenderingMode: vi.fn(),
    onClose: vi.fn(async () => true), onOpenPanel: vi.fn(async () => true)};
}

it("collapses advanced settings and only exposes mobile controls for an attached supported gamepad", async () => {
  let connected = false;
  Object.defineProperty(navigator, "getGamepads", {configurable: true, value: () => connected ? [{connected: true}] : []});
  const values = props();
  const view = render(<PlayerEmulatorSettings {...values} />);
  expect(screen.queryByRole("button", {name: "显示"})).toBeNull();
  await userEvent.setup().click(screen.getByRole("button", {name: "高级设置"}));
  expect(screen.getByRole("button", {name: "显示"})).toBeVisible();
  expect(screen.queryByRole("button", {name: "控制"})).toBeNull();
  act(() => {connected = true; window.dispatchEvent(new Event("gamepadconnected"));});
  expect(screen.getByRole("button", {name: "控制"})).toBeVisible();
  view.rerender(<PlayerEmulatorSettings {...values} capabilities={{...values.capabilities, standardGamepad: false}} />);
  expect(screen.queryByRole("button", {name: "控制"})).toBeNull();
  view.rerender(<PlayerEmulatorSettings {...values} />);
  act(() => {connected = false; window.dispatchEvent(new Event("gamepaddisconnected"));});
  expect(screen.queryByRole("button", {name: "控制"})).toBeNull();
});

it("keeps navigation usable after failed open or close and ignores duplicate operations", async () => {
  const values = {...props(), mobile: false};
  let finish: (value: boolean) => void = () => {};
  values.onOpenPanel.mockImplementationOnce(() => new Promise<boolean>((resolve) => {finish = resolve;}));
  render(<PlayerEmulatorSettings {...values} />);
  const display = screen.getByRole("button", {name: "显示"});
  fireEvent.click(display);
  fireEvent.click(display);
  expect(values.onOpenPanel).toHaveBeenCalledOnce();
  expect(display).toBeDisabled();
  await act(async () => finish(false));
  expect(display).toBeEnabled();
  expect(screen.queryByRole("region", {name: "原生设置导航"})).toBeNull();
  const user = userEvent.setup();
  await user.click(display);
  expect(screen.getByRole("button", {name: "返回设置"})).toHaveFocus();
  values.onOpenPanel.mockResolvedValueOnce(false);
  await user.keyboard("{Escape}");
  expect(screen.getByRole("region", {name: "原生设置导航"})).toBeVisible();
  await user.keyboard("{Escape}");
  expect(display).not.toBeInTheDocument();
  expect(screen.getByRole("button", {name: "显示"})).toHaveFocus();
  values.onClose.mockResolvedValueOnce(false);
  await user.keyboard("{Escape}");
  expect(screen.getByRole("region", {name: "模拟器设置工具栏"})).toBeVisible();
  expect(screen.getByRole("button", {name: "收起"})).toBeEnabled();
});
