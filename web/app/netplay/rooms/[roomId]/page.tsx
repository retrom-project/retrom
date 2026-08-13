import { notFound } from "next/navigation";
import { loadAuthContext } from "@/features/auth/server";
import { NetplayRoomLobby, type NetplayFilterParams } from "@/features/netplay/room-lobby";
import type { NetplayGameList, NetplayRoom } from "@/features/netplay/client";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "联机房间" };

async function loadGames() {
  const items: NetplayGameList["items"] = [];
  let cursor: string | null = null;
  do {
    const page: NetplayGameList = await backendJSON<NetplayGameList>(`/api/v1/netplay/games?availability=ALL&limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`);
    items.push(...page.items); cursor = page.nextCursor;
  } while (cursor);
  return items;
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
  if (!context.netplayEnabled) notFound();
  const [{ roomId }, query] = await Promise.all([params, searchParams]);
  const [room, games] = await Promise.all([
    backendJSON<NetplayRoom>(`/api/v1/netplay/rooms/${encodeURIComponent(roomId)}`),
    loadGames(),
  ]);
  const initialFilterParams: NetplayFilterParams = {
    q: first(query, "q"), platformId: first(query, "platformId"),
    platformInstanceId: first(query, "platformInstanceId"),
    availability: first(query, "availability"), sort: first(query, "sort"),
  };
  return <NetplayRoomLobby initialRoom={room} games={games} initialFilterParams={initialFilterParams} />;
}
