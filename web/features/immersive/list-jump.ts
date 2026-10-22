export type JumpAction = "pagePrevious" | "pageNext" | "groupPrevious" | "groupNext";

export function isJumpAction(action: string): action is JumpAction {
  return action === "pagePrevious" || action === "pageNext" || action === "groupPrevious" || action === "groupNext";
}

/** Where a letter group starts in the full, title-ordered list (from the letters API). */
export type LetterStart = Readonly<{ letter: string; offset: number; count: number }>;

export type JumpTarget = Readonly<{ index: number; loadMore: boolean }>;

/** Move one page within the loaded window; past its end it stops on the last item and loads more. */
export function pageTarget(selected: number, pageSize: number, direction: "previous" | "next", loaded: number, hasMore: boolean): JumpTarget {
  const last = Math.max(0, loaded - 1);
  if (direction === "previous") {
    return { index: Math.max(0, selected - pageSize), loadMore: false };
  }
  const index = Math.min(last, selected + pageSize);
  return { index, loadMore: hasMore && selected + pageSize >= last };
}

/** The letter group containing a list position. */
export function letterAt(letters: readonly LetterStart[], position: number) {
  return letters.findLast((letter) => letter.offset <= position)?.letter ?? "";
}

/**
 * The list offset of the neighbouring letter group. Going back first returns to the start of the
 * current group; there is no target before the first or after the last group.
 */
export function letterTarget(letters: readonly LetterStart[], position: number, direction: "previous" | "next") {
  if (direction === "next") {
    return letters.find((letter) => letter.offset > position)?.offset ?? null;
  }
  const current = letters.findLastIndex((letter) => letter.offset <= position);
  if (current < 0) { return null; }
  if (letters[current].offset < position) { return letters[current].offset; }
  return current > 0 ? letters[current - 1].offset : null;
}
