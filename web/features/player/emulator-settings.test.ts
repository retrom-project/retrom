import { afterEach, describe, expect, it, vi } from "vitest";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
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

function nestedInstance(ownerDocument: Document = document): EmulatorInstance {
  const settingsMenu = ownerDocument.createElement("div");
  const transition = ownerDocument.createElement("div");
  transition.className = "ejs_settings_transition";
  const corePanel = ownerDocument.createElement("div");
  corePanel.dataset.panel = "core";
  const displayPanel = ownerDocument.createElement("div");
  displayPanel.dataset.panel = "display";
  displayPanel.setAttribute("hidden", "");
  const home = ownerDocument.createElement("div");
  home.className = "ejs_setting_menu";
  home.setAttribute("hidden", "");
  for (const [label, panel] of [["Graphics Settings", displayPanel], ["Backend Core Options", corePanel]] as const) {
    const button = ownerDocument.createElement("button");
    button.className = "ejs_settings_main_bar";
    button.textContent = label;
    button.addEventListener("click", () => {
      home.setAttribute("hidden", "");
      panel.removeAttribute("hidden");
    });
    home.append(button);
  }
  transition.append(corePanel, displayPanel, home);
  settingsMenu.append(transition);
  return { on: () => undefined, menu: { open: vi.fn(), close: vi.fn() }, settingsMenu };
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

  it("resets the native nested page before switching from core options to graphics", () => {
    const frame = document.createElement("iframe");
    document.body.append(frame);
    const emulator = nestedInstance(frame.contentDocument!);
    expect(emulator.settingsMenu?.querySelector<HTMLElement>("[data-panel=core]")).not.toHaveAttribute("hidden");
    expect(openEmulatorSettingsPanel(emulator, "display")).toBe(true);
    expect(emulator.settingsMenu?.querySelector<HTMLElement>("[data-panel=core]")).toHaveAttribute("hidden");
    expect(emulator.settingsMenu?.querySelector<HTMLElement>("[data-panel=display]")).not.toHaveAttribute("hidden");
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
