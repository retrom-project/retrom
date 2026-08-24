let activeImmersiveGamepadIndex: number | null = null;

export function getActiveImmersiveGamepadIndex() {
  return activeImmersiveGamepadIndex;
}

export function setActiveImmersiveGamepadIndex(index: number | null) {
  activeImmersiveGamepadIndex = index;
}
