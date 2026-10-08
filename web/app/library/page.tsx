import { LibraryBrowser } from "@/features/library/library-browser";
import type { ListFilters } from "@/features/library/library-browser";
export default async function Page({
  searchParams,
}: {
  searchParams: Promise<ListFilters>;
}) {
  return <LibraryBrowser kind="library" initial={await searchParams} />;
}
