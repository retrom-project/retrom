let activeImmersiveGamepadIndex: number | null = null;
let immersivePlayerReturnPending = false;

export function getActiveImmersiveGamepadIndex() {
  return activeImmersiveGamepadIndex;
}

export function setActiveImmersiveGamepadIndex(index: number | null) {
  activeImmersiveGamepadIndex = index;
}

export function markImmersivePlayerReturn() {
  immersivePlayerReturnPending = true;
}

export function isImmersivePlayerReturnPending() {
  return immersivePlayerReturnPending;
}

export function consumeImmersivePlayerReturn() {
  const pending = immersivePlayerReturnPending;
  immersivePlayerReturnPending = false;
  return pending;
}
