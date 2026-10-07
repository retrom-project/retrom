import type { NavigationAction } from "@/features/immersive/input-model";

export function focusPlayerMenu() {
  menuControls()[0]?.focus();
}

export function navigatePlayerMenu(action: NavigationAction) {
  const controls = menuControls();
  const current = document.activeElement;
  const index = controls.findIndex((control) => control === current);
  if (action === "confirm") {
    if (current instanceof HTMLButtonElement) {
      current.click();
    }
    return;
  }
  if (action === "left" || action === "right") {
    adjustControl(current, action === "left" ? -1 : 1);
    return;
  }
  if (action === "up" || action === "down") {
    const direction = action === "up" ? -1 : 1;
    controls[(index + direction + controls.length) % controls.length]?.focus();
  }
}

function menuControls() {
  return [
    ...document.querySelectorAll<HTMLElement>(
      ".player-menu button:not(:disabled), .player-menu input, .player-menu select",
    ),
  ].filter((control) => control.getClientRects().length > 0);
}

function adjustControl(element: Element | null, direction: number) {
  if (element instanceof HTMLSelectElement) {
    element.selectedIndex = Math.max(
      0,
      Math.min(element.options.length - 1, element.selectedIndex + direction),
    );
    element.dispatchEvent(new Event("change", { bubbles: true }));
  }
  if (element instanceof HTMLInputElement && element.type === "range") {
    const value = String(
      Math.max(0, Math.min(100, Number(element.value) + direction * 5)),
    );
    Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      "value",
    )?.set?.call(element, value);
    element.dispatchEvent(new Event("input", { bubbles: true }));
    element.dispatchEvent(new Event("change", { bubbles: true }));
  }
}
