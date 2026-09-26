export type ImmersiveMenuSelection = 0 | 1 | 2 | 3;

export function moveImmersiveMenuSelection(
  current: ImmersiveMenuSelection,
  direction: "left" | "right",
  saveAvailable: boolean,
  editorAvailable = false,
): ImmersiveMenuSelection {
  const choices: ImmersiveMenuSelection[] = [0, ...(saveAvailable ? [1] : []), ...(editorAvailable ? [3] : []), 2] as ImmersiveMenuSelection[];
  const currentIndex = Math.max(0, choices.indexOf(current));
  const offset = direction === "right" ? 1 : choices.length - 1;
  return choices[(currentIndex + offset) % choices.length];
}

export function selectableImmersiveMenuItem(selected: ImmersiveMenuSelection, saveAvailable: boolean, editorAvailable = false) {
  return selected === 1 ? saveAvailable : selected === 3 ? editorAvailable : true;
}
