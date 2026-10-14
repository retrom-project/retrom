/** The compact landscape editor scrolls its whole content area, including search and actor controls. */
export function gameEditorScrollRoot(list: HTMLElement) {
  const content = list.closest<HTMLElement>(".game-editor-content");
  return content && getComputedStyle(content).overflowY === "auto" ? content : list;
}
export function resetGameEditorScroll(list: HTMLElement | null) {
  if (!list) {return;}
  list.scrollTop = 0;
  const content = list.closest<HTMLElement>(".game-editor-content");
  if (content) {content.scrollTop = 0;}
}
