export type ImmersiveMenuSelection = 0 | 1 | 2 | 3;

export function moveImmersiveMenuSelection(
  current: ImmersiveMenuSelection,
  direction: "left" | "right",
  saveAvailable: boolean,
  cursorAvailable = false,
): ImmersiveMenuSelection {
  const choices: ImmersiveMenuSelection[] = [0];
  if (cursorAvailable) {choices.push(3);}
  if (saveAvailable) {choices.push(1);}
  choices.push(2);
  const currentIndex = Math.max(0, choices.indexOf(current));
  const offset = direction === "right" ? 1 : choices.length - 1;
  return choices[(currentIndex + offset) % choices.length];
}

export function selectableImmersiveMenuItem(selected: ImmersiveMenuSelection, saveAvailable: boolean, cursorAvailable = false) {
  return (selected !== 1 || saveAvailable) && (selected !== 3 || cursorAvailable);
}
