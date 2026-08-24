import { notFound } from "next/navigation";
import { LibraryGameListView } from "@/features/immersive/library-game-list-view";
import type { ImmersiveLibraryKind } from "@/features/immersive/api";

export const metadata = { title: "游戏资料库 · 沉浸模式" };

function libraryKind(value: string): ImmersiveLibraryKind | null {
  return value === "all" || value === "recent" || value === "favorites" || value === "saves" ? value : null;
}

function first(value: string | string[] | undefined) {
  return Array.isArray(value) ? value[0] : value;
}

export default async function ImmersiveLibraryPage({ params, searchParams }: {
  params: Promise<{ kind: string }>;
  searchParams: Promise<{
    folderId?: string | string[];
    gameId?: string | string[];
    saveStateId?: string | string[];
  }>;
}) {
  const [{ kind: rawKind }, query] = await Promise.all([params, searchParams]);
  const kind = libraryKind(rawKind);
  if (!kind) {notFound();}
  return <LibraryGameListView
    kind={kind}
    folderId={first(query.folderId)}
    initialGameId={first(query.gameId)}
    initialSaveStateId={first(query.saveStateId)}
  />;
}
