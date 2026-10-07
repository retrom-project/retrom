"use client";
import { useState } from "react";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { toggleFavorite } from "@/features/library/api";
async function loadFolders() {
  return result(await api.GET("/api/v1/favorite-folders"));
}
export function FavoriteOrganizer({
  detail,
  onChange,
}: {
  detail: Schema<"GameDetail">;
  onChange: () => void;
}) {
  const folders = useResource(loadFolders);
  const [error, setError] = useState("");
  async function change(id: string, checked: boolean) {
    const folderIds = checked
      ? [...detail.favoriteFolderIds, id]
      : detail.favoriteFolderIds.filter((value) => value !== id);
    try {
      await toggleFavorite(detail.game.id, true, folderIds);
      onChange();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "操作失败。");
    }
  }
  return (
    <details className="favorite-organizer">
      <summary>整理收藏夹</summary>
      <fieldset>
        <legend>收藏夹</legend>
        <div className="stack">
          {folders.data?.items.map((folder) => (
            <label key={folder.id}>
              <input
                type="checkbox"
                checked={detail.favoriteFolderIds.includes(folder.id)}
                onChange={(event) =>
                  void change(folder.id, event.target.checked)
                }
              />
              {folder.name}
            </label>
          ))}
        </div>
        {error || folders.error ? (
          <p role="alert">{error || folders.error}</p>
        ) : null}
      </fieldset>
    </details>
  );
}
