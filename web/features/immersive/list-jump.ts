export type JumpAction = "pagePrevious" | "pageNext" | "groupPrevious" | "groupNext";

export function isJumpAction(action: string): action is JumpAction {
  return action === "pagePrevious" || action === "pageNext" || action === "groupPrevious" || action === "groupNext";
}

/** Lists are ordered by the first character of the trimmed title, upper-cased, as the server stores it. */
export function titleGroup(title: string) {
  const first = [...title.trim()][0];
  return first ? first.toUpperCase() : "";
}

export type JumpTarget = Readonly<{ index: number; loadMore: boolean }>;

/** Move one page; past the loaded end it stops on the last item and asks for the next batch. */
export function pageTarget(selected: number, pageSize: number, direction: "previous" | "next", loaded: number, hasMore: boolean): JumpTarget {
  const last = Math.max(0, loaded - 1);
  if (direction === "previous") {
    return { index: Math.max(0, selected - pageSize), loadMore: false };
  }
  const index = Math.min(last, selected + pageSize);
  return { index, loadMore: hasMore && selected + pageSize >= last };
}

/**
 * Jump to the first title of the neighbouring group. Going back first returns to the start of the
 * current group. A next group that is not loaded yet keeps the last item selected and loads more.
 */
export function groupTarget(titles: readonly string[], selected: number, direction: "previous" | "next", hasMore: boolean): JumpTarget {
  const current = titleGroup(titles[selected] ?? "");
  if (direction === "next") {
    for (let index = selected + 1; index < titles.length; index++) {
      if (titleGroup(titles[index]) !== current) { return { index, loadMore: false }; }
    }
    return { index: Math.max(0, titles.length - 1), loadMore: hasMore };
  }
  let start = selected;
  while (start > 0 && titleGroup(titles[start - 1]) === current) { start--; }
  if (start < selected || start === 0) { return { index: start, loadMore: false }; }
  const previous = titleGroup(titles[start - 1]);
  let index = start - 1;
  while (index > 0 && titleGroup(titles[index - 1]) === previous) { index--; }
  return { index, loadMore: false };
}
