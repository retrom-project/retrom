export type ImmersiveMenuSelection = 0 | 1 | 2;

export function moveImmersiveMenuSelection(
  current: ImmersiveMenuSelection,
  direction: "left" | "right",
  saveAvailable: boolean,
): ImmersiveMenuSelection {
  const choices: ImmersiveMenuSelection[] = saveAvailable ? [0, 1, 2] : [0, 2];
  const currentIndex = Math.max(0, choices.indexOf(current));
  const offset = direction === "right" ? 1 : choices.length - 1;
  return choices[(currentIndex + offset) % choices.length];
}

export function selectableImmersiveMenuItem(selected: ImmersiveMenuSelection, saveAvailable: boolean) {
  return selected !== 1 || saveAvailable;
}
