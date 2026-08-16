import { LibraryBrowser } from "@/features/library/library-browser";
import { gamePageQuery, type GamePage, type LibraryFilters } from "@/features/library/game-library";
import { scalarSearchParams } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "游戏库" };

export default async function LibraryPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "platformId", "platformInstanceId", "tagId", "sort"]);
  const sort: LibraryFilters["sort"] = values.sort === "ADDED_DESC" || values.sort === "TITLE_ASC" ? values.sort : "RECENT_DESC";
  const initialFilters: LibraryFilters = {
    query: values.q ?? "",
    platformId: values.platformId ?? "",
    platformInstanceId: values.platformInstanceId ?? "",
    tagId: values.tagId ?? "",
    sort,
  };
  const library = await backendJSON<GamePage>(`/api/v1/games?${gamePageQuery(initialFilters)}`);
  return <LibraryBrowser initialPage={library} initialFilters={initialFilters} />;
}
