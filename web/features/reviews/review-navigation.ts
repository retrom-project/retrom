export function adjacentReviewItemId(
  itemIds: string[],
  currentItemId: string,
): string | null {
  const currentIndex = itemIds.indexOf(currentItemId);
  if (currentIndex < 0) return itemIds.find((itemId) => itemId !== currentItemId) ?? null;
  return itemIds[currentIndex + 1] ?? itemIds[currentIndex - 1] ?? null;
}
