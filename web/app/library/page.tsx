import { LibraryBrowser } from "@/features/library/library-browser";
import { collectGamePages, type GamePage, type LibraryFilters } from "@/features/library/game-library";
import { scalarSearchParams, withQuery } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "游戏库" };

async function loadAllGames() {
  return collectGamePages((cursor) => backendJSON<GamePage>(withQuery("/api/v1/games", {
    limit: "100",
    ...(cursor ? { cursor } : {}),
  })));
}

export default async function LibraryPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "platformId", "platformInstanceId", "tagId", "sort"]);
  const sort: LibraryFilters["sort"] = values.sort === "ADDED_DESC" || values.sort === "TITLE_ASC" ? values.sort : "RECENT_DESC";
  const library = await loadAllGames();
  return <LibraryBrowser games={library.items} nowMs={library.generatedAtMs} initialFilters={{
    query: values.q ?? "",
    platformId: values.platformId ?? "",
    platformInstanceId: values.platformInstanceId ?? "",
    tagId: values.tagId ?? "",
    sort,
  }} />;
}
