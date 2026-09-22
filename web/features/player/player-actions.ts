export type PlayerContextAction = "disc" | "save";

export function playerActionPriority({ disc, save }: { disc: boolean; save: boolean }) {
  const ordered: PlayerContextAction[] = [];
  if (save) {ordered.push("save");}
  if (disc) {ordered.push("disc");}
  return { primary: ordered[0] ?? null, overflow: ordered.slice(1) };
}
