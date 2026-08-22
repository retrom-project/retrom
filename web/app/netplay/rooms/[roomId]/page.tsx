import { notFound } from "next/navigation";
import { loadAuthContext } from "@/features/auth/server";
import { NetplayRoomLobby, type NetplayFilterParams } from "@/features/netplay/room-lobby";
import type { NetplayGameList, NetplayRoom } from "@/features/netplay/client";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "联机房间" };

async function loadGamePage() {
  return backendJSON<NetplayGameList>("/api/v1/netplay/games?availability=ALL&limit=100");
}

type PageSearchParams = Promise<Record<string, string | string[] | undefined>>;

function first(values: Record<string, string | string[] | undefined>, name: keyof NetplayFilterParams) {
  const value = values[name];
  return Array.isArray(value) ? value[0] : value;
}

export default async function NetplayRoomPage({ params, searchParams }: {
  params: Promise<{ roomId: string }>;
  searchParams: PageSearchParams;
}) {
  const context = await loadAuthContext();
  if (!context.netplayEnabled) {notFound();}
  const [{ roomId }, query] = await Promise.all([params, searchParams]);
  const room = await backendJSON<NetplayRoom>(`/api/v1/netplay/rooms/${encodeURIComponent(roomId)}`);
  const gamePage = room.state === "DRAFT" && room.permissions.canSelectGame
    ? await loadGamePage()
    : { items: [], nextCursor: null };
  const initialFilterParams: NetplayFilterParams = {
    q: first(query, "q"), platformId: first(query, "platformId"),
    platformInstanceId: first(query, "platformInstanceId"),
    availability: first(query, "availability"), sort: first(query, "sort"),
  };
  return <NetplayRoomLobby
    initialRoom={room}
    games={gamePage.items}
    initialGamesNextCursor={gamePage.nextCursor}
    initialFilterParams={initialFilterParams}
  />;
}
