import { SaveManager } from "@/features/saves/save-manager";
import { collectSavePages, type SaveFilters, type SavePage } from "@/features/saves/save-library";
import { backendJSON, scalarSearchParams, withQuery } from "@/lib/backend";

export const metadata = { title: "我的存档" };

async function loadAllSaves() {
  return collectSavePages((cursor) => backendJSON<SavePage>(withQuery("/api/v1/saves", {
    availability: "ALL",
    limit: "100",
    ...(cursor ? { cursor } : {}),
  })));
}

export default async function SavesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "gameId", "availability", "sort"]);
  const availability: SaveFilters["availability"] = values.availability === "ALL" || values.availability === "BLOCKED" ? values.availability : "AVAILABLE";
  const sort: SaveFilters["sort"] = values.sort === "CREATED_ASC" ? "CREATED_ASC" : "CREATED_DESC";
  const saves = await loadAllSaves();
  return <SaveManager saves={saves.items} nowMs={saves.generatedAtMs} initialFilters={{ query: values.q ?? "", gameId: values.gameId ?? "", availability, sort }} />;
}
