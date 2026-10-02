import { SaveManager } from "@/features/saves/save-manager";
import { collectSavePages, type SavePage } from "@/features/saves/save-library";
import { withQuery } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "我的存档" };

async function loadAllSaves() {
  return collectSavePages((cursor) => backendJSON<SavePage>(withQuery("/api/v1/saves", {
    availability: "ALL",
    limit: "100",
    ...(cursor ? { cursor } : {}),
  })));
}

export default async function SavesPage() {
  const saves = await loadAllSaves();
  return <SaveManager saves={saves.items} nowMs={saves.generatedAtMs} />;
}
