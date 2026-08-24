import type { Page } from "@playwright/test";

declare global {
  interface Window {
    __retromE2EGamepads?: Array<{
      axes: number[];
      buttons: Array<{ pressed: boolean; touched: boolean; value: number }>;
      connected: boolean;
      id: string;
      index: number;
      mapping: GamepadMappingType;
      timestamp: number;
    }>;
  }
}

const buttonCount = 17;

export async function installGamepads(page: Page, count = 1) {
  await page.addInitScript(({ buttons, gamepadCount }) => {
    const owner = window.top ?? window;
    if (!owner.__retromE2EGamepads) {
      owner.__retromE2EGamepads = Array.from({ length: gamepadCount }, (_, index) => ({
        axes: [0, 0, 0, 0],
        buttons: Array.from({ length: buttons }, () => ({ pressed: false, touched: false, value: 0 })),
        connected: true,
        id: `Retrom Standard Test Pad ${index + 1}`,
        index,
        mapping: "standard" as GamepadMappingType,
        timestamp: 0,
      }));
    }
    Object.defineProperty(navigator, "getGamepads", {
      configurable: true,
      value: () => owner.__retromE2EGamepads ?? [],
    });
  }, { buttons: buttonCount, gamepadCount: count });
}

export async function setGamepadButtons(page: Page, gamepadIndex: number, pressed: readonly number[]) {
  await page.evaluate(({ index, pressedButtons }) => {
    const owner = window.top ?? window;
    const gamepad = owner.__retromE2EGamepads?.find((candidate) => candidate.index === index);
    if (!gamepad) {throw new Error(`E2E_GAMEPAD_NOT_FOUND:${index}`);}
    const active = new Set(pressedButtons);
    gamepad.buttons.forEach((button, buttonIndex) => {
      button.pressed = active.has(buttonIndex);
      button.touched = active.has(buttonIndex);
      button.value = active.has(buttonIndex) ? 1 : 0;
    });
    gamepad.timestamp += 1;
  }, { index: gamepadIndex, pressedButtons: [...pressed] });
}

export async function setGamepadConnected(page: Page, gamepadIndex: number, connected: boolean) {
  await page.evaluate(({ index, nextConnected }) => {
    const owner = window.top ?? window;
    const gamepad = owner.__retromE2EGamepads?.find((candidate) => candidate.index === index);
    if (!gamepad) {throw new Error(`E2E_GAMEPAD_NOT_FOUND:${index}`);}
    gamepad.connected = nextConnected;
    gamepad.timestamp += 1;
  }, { index: gamepadIndex, nextConnected: connected });
}

export async function pressGamepad(
  page: Page,
  buttons: number | readonly number[],
  gamepadIndex = 0,
  holdMs = 140,
  releaseMs = 180,
) {
  const pressed = typeof buttons === "number" ? [buttons] : buttons;
  await setGamepadButtons(page, gamepadIndex, pressed);
  await page.waitForTimeout(holdMs);
  await setGamepadButtons(page, gamepadIndex, []);
  await page.waitForTimeout(releaseMs);
}

export const standardButton = {
  a: 0,
  b: 1,
  y: 3,
  select: 8,
  start: 9,
  up: 12,
  down: 13,
  left: 14,
  right: 15,
} as const;
