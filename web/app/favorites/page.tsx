import { FavoriteBrowser } from "@/features/favorites/favorite-browser";
import type { FavoritePage } from "@/features/favorites/favorite-api";
import { favoriteQuery, favoriteQueryString } from "@/features/favorites/favorite-state";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "我的收藏" };

export default async function FavoritesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const query = favoriteQuery(await searchParams);
  let initialPage: FavoritePage | null = null;
  let initialError = "";
  try {
    initialPage = await backendJSON<FavoritePage>(`/api/v1/favorites?${favoriteQueryString(query)}`);
  } catch {
    initialError = query.scope === "FOLDER" ? "收藏夹不存在或暂时无法读取。" : "收藏数据暂时无法加载，请重试。";
  }
  return <FavoriteBrowser initialPage={initialPage} initialQuery={query} initialError={initialError} />;
}
