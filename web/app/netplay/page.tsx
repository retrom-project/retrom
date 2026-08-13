import { notFound } from "next/navigation";
import { loadAuthContext } from "@/features/auth/server";
import { NetplayRoomList } from "@/features/netplay/room-list";
import type { NetplayRoomList as RoomList } from "@/features/netplay/client";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "联机游玩" };

export default async function NetplayPage() {
  const context = await loadAuthContext();
  if (!context.netplayEnabled) notFound();
  const [active, recent] = await Promise.all([
    backendJSON<RoomList>("/api/v1/netplay/rooms?view=active&limit=24"),
    backendJSON<RoomList>("/api/v1/netplay/rooms?view=recent&limit=24"),
  ]);
  return <NetplayRoomList active={active.items} recent={recent.items} />;
}
