import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";

export type EmulatorSettingsPanel = "controls" | "display" | "core";

const panelMatchers: Record<Exclude<EmulatorSettingsPanel, "controls">, RegExp> = {
  display: /graphics settings|display settings|图形设置|图像设置|显示设置/i,
  core: /backend core options|core options|核心选项|核心设置/i,
};

function hideControlPanel(instance: EmulatorInstance) {
  if (instance.controlMenu) {instance.controlMenu.style.display = "none";}
}

function setNativeSettingsVisibility(instance: EmulatorInstance, visible: boolean) {
  const frameDocument = instance.settingsMenu?.ownerDocument ?? instance.controlMenu?.ownerDocument;
  frameDocument?.documentElement.classList.toggle("retrom-native-settings-open", visible);
}

function resetNativeSettingsNavigation(instance: EmulatorInstance) {
  const transition = instance.settingsMenu?.querySelector<HTMLElement>(".ejs_settings_transition");
  if (!transition) {return instance.settingsMenu ?? null;}
  // The native settings tree lives in the same-origin Player iframe. Avoid an
  // instanceof check against the host realm, which rejects iframe elements.
  const children = [...transition.querySelectorAll<HTMLElement>(":scope > *")];
  const home = children.find((child) => child.classList.contains("ejs_setting_menu"));
  if (!home) {return instance.settingsMenu ?? null;}
  for (const child of children) {
    if (child === home) {child.removeAttribute("hidden");}
    else {child.setAttribute("hidden", "");}
  }
  return home;
}

export function openEmulatorSettingsPanel(instance: EmulatorInstance, panel: EmulatorSettingsPanel) {
  if (panel === "controls") {
    setNativeSettingsVisibility(instance, false);
    instance.closeSettingsMenu?.();
    instance.menu?.close?.();
    if (!instance.controlMenu) {return false;}
    instance.controlMenu.style.display = "";
    return true;
  }

  hideControlPanel(instance);
  if (!instance.settingsMenu) {return false;}
  setNativeSettingsVisibility(instance, true);
  instance.menu?.open?.(true);
  instance.settingsMenuOpen = true;
  instance.settingsMenu.style.display = "";
  const matcher = panelMatchers[panel];
  const navigationRoot = resetNativeSettingsNavigation(instance);
  const target = [...(navigationRoot?.querySelectorAll<HTMLElement>(".ejs_settings_main_bar") ?? [])]
    .find((entry) => matcher.test(entry.textContent ?? ""));
  if (!target) {return false;}
  target.click();
  return true;
}

export function closeEmulatorSettingsPanels(instance: EmulatorInstance | undefined) {
  if (!instance) {return;}
  hideControlPanel(instance);
  setNativeSettingsVisibility(instance, false);
  instance.closeSettingsMenu?.();
  instance.menu?.close?.();
}
