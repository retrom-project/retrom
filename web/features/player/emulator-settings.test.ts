import { afterEach, describe, expect, it, vi } from "vitest";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v1";
import { closeEmulatorSettingsPanels, openEmulatorSettingsPanel } from "./emulator-settings";

afterEach(() => document.documentElement.classList.remove("retrom-native-settings-open"));

function instance(): EmulatorInstance {
  const settingsMenu = document.createElement("div");
  const controlMenu = document.createElement("div");
  controlMenu.style.display = "none";
  for (const label of ["Graphics Settings", "Backend Core Options"]) {
    const button = document.createElement("button");
    button.className = "ejs_settings_main_bar";
    button.textContent = label;
    settingsMenu.append(button);
  }
  return {
    on: () => undefined,
    menu: { open: vi.fn(), close: vi.fn() },
    controlMenu,
    settingsMenu,
    closeSettingsMenu: vi.fn(() => { settingsMenu.style.display = "none"; }),
  };
}

describe("EmulatorJS settings bridge", () => {
  it("opens the real control panel without exposing the native menu bar", () => {
    const emulator = instance();
    expect(openEmulatorSettingsPanel(emulator, "controls")).toBe(true);
    expect(emulator.controlMenu?.style.display).toBe("");
    expect(emulator.menu?.open).not.toHaveBeenCalled();
  });

  it.each([
    ["display", "Graphics Settings"],
    ["core", "Backend Core Options"],
  ] as const)("routes %s to the matching real settings page", (panel, label) => {
    const emulator = instance();
    const target = [...emulator.settingsMenu!.querySelectorAll<HTMLElement>(".ejs_settings_main_bar")]
      .find((entry) => entry.textContent === label)!;
    const click = vi.spyOn(target, "click");
    expect(openEmulatorSettingsPanel(emulator, panel)).toBe(true);
    expect(emulator.menu?.open).toHaveBeenCalledWith(true);
    expect(document.documentElement).toHaveClass("retrom-native-settings-open");
    expect(emulator.settingsMenu?.style.display).toBe("");
    expect(click).toHaveBeenCalledOnce();
  });

  it("closes all native panels behind the Retrom toolbar", () => {
    const emulator = instance();
    emulator.controlMenu!.style.display = "";
    emulator.settingsMenu!.style.display = "";
    closeEmulatorSettingsPanels(emulator);
    expect(emulator.controlMenu?.style.display).toBe("none");
    expect(emulator.closeSettingsMenu).toHaveBeenCalledOnce();
    expect(emulator.menu?.close).toHaveBeenCalledOnce();
    expect(document.documentElement).not.toHaveClass("retrom-native-settings-open");
  });
});
