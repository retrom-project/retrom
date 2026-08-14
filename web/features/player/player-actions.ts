export type PlayerContextAction = "netplay" | "disc" | "save";

export function playerActionPriority({ netplay, disc, save }: { netplay: boolean; disc: boolean; save: boolean }) {
  const ordered: PlayerContextAction[] = [];
  if (netplay) ordered.push("netplay");
  if (disc) ordered.push("disc");
  if (save) ordered.push("save");
  return { primary: ordered[0] ?? null, overflow: ordered.slice(1) };
}
