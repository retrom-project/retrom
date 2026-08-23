import type { Page } from "@playwright/test";

type SyntheticPad = Readonly<{
  index: number;
  connected: boolean;
  mapping: GamepadMappingType | "";
  buttons: readonly number[];
  axes: readonly number[];
}>;

const storageKey = "__retrom_e2e_gamepads";

export function standardPad(index = 0, pressed: readonly number[] = []): SyntheticPad {
  const active = new Set(pressed);
  return {
    index,
    connected: true,
    mapping: "standard",
    buttons: Array.from({ length: 17 }, (_, button) => active.has(button) ? 1 : 0),
    axes: [0, 0, 0, 0],
  };
}

export function unknownPad(index = 0, pressed: readonly number[] = []): SyntheticPad {
  return { ...standardPad(index, pressed), mapping: "" };
}

export async function installSyntheticGamepads(page: Page) {
  await page.addInitScript(({ key }) => {
    let sequence = 0;
    const readPads = () => {
      let values: SyntheticPad[] = [];
      try {
        values = JSON.parse(localStorage.getItem(key) ?? "[]") as SyntheticPad[];
      } catch {
        values = [];
      }
      sequence += 1;
      const result: Array<Gamepad | null> = [];
      for (const value of values) {
        result[value.index] = {
          id: "Retrom synthetic standard controller",
          index: value.index,
          connected: value.connected,
          mapping: value.mapping,
          timestamp: sequence,
          buttons: value.buttons.map((button) => ({
            pressed: button >= 0.5,
            touched: button > 0,
            value: button,
          })),
          axes: [...value.axes],
          vibrationActuator: null,
        } as unknown as Gamepad;
      }
      return result;
    };
    Object.defineProperty(Navigator.prototype, "getGamepads", {
      configurable: true,
      value: readPads,
    });
  }, { key: storageKey });
}

export async function setSyntheticGamepads(page: Page, pads: readonly SyntheticPad[]) {
  await page.evaluate(({ key, values }) => {
    localStorage.setItem(key, JSON.stringify(values));
  }, { key: storageKey, values: pads });
}

export async function neutralGamepad(page: Page, index = 0, waitMs = 150) {
  await setSyntheticGamepads(page, [standardPad(index)]);
  await page.waitForTimeout(waitMs);
}

export async function pressGamepadButton(
  page: Page,
  button: number,
  options: Readonly<{ index?: number; holdMs?: number; releaseMs?: number }> = {},
) {
  const index = options.index ?? 0;
  await setSyntheticGamepads(page, [standardPad(index, [button])]);
  await page.waitForTimeout(options.holdMs ?? 50);
  await setSyntheticGamepads(page, [standardPad(index)]);
  await page.waitForTimeout(options.releaseMs ?? 40);
}

export async function pressGamepadDirection(
  page: Page,
  direction: "up" | "down" | "left" | "right",
  index = 0,
) {
  const buttons = { up: 12, down: 13, left: 14, right: 15 } as const;
  await pressGamepadButton(page, buttons[direction], { index });
}

export async function pressGamepadCombination(page: Page, index = 0, holdMs = 50) {
  await setSyntheticGamepads(page, [standardPad(index, [8, 9])]);
  await page.waitForTimeout(holdMs);
  await setSyntheticGamepads(page, [standardPad(index)]);
  await page.waitForTimeout(50);
}

export async function claimGamepad(page: Page, index = 0) {
  await pressGamepadButton(page, 0, { index, holdMs: 50, releaseMs: 0 });
  await neutralGamepad(page, index, 160);
}

export async function focusedControl(page: Page) {
  return page.evaluate(() => {
    const active = document.activeElement as HTMLElement | null;
    return {
      tag: active?.tagName ?? "",
      label: active?.getAttribute("aria-label") ?? active?.textContent?.trim() ?? "",
      href: active instanceof HTMLAnchorElement ? active.getAttribute("href") : null,
    };
  });
}
