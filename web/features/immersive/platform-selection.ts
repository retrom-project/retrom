export function wrapPlatformIndex(current: number, direction: "left" | "right", count: number) {
  if (count <= 0) {return -1;}
  const delta = direction === "left" ? -1 : 1;
  return (current + delta + count) % count;
}
