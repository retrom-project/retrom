import { LibraryBrowser } from "@/features/library/library-browser";
import { gamePageQuery, type GamePage } from "@/features/library/game-library";
import { libraryURLFilters } from "@/features/library/library-url";
import { scalarSearchParams } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "游戏库" };

export default async function LibraryPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "platformId", "platformInstanceId", "tagId", "sort"]);
  const initialFilters = libraryURLFilters(new URLSearchParams(values));
  const library = await backendJSON<GamePage>(`/api/v1/games?${gamePageQuery(initialFilters)}`);
  return <LibraryBrowser initialPage={library} initialFilters={initialFilters} />;
}
