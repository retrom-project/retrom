import { FavoriteLibrary } from "@/features/favorites/favorite-library";
import type { ListFilters } from "@/features/library/library-browser";
export default async function Page({
  searchParams,
}: {
  searchParams: Promise<ListFilters>;
}) {
  return <FavoriteLibrary initial={await searchParams} />;
}
