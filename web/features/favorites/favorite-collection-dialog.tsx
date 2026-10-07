"use client";
import { useCallback, useState } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { loadDetail, toggleFavorite } from "@/features/library/api";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ResourceState } from "@/components/resource-state";
import type { Schema } from "@/lib/api/types";
export function FavoriteCollectionDialog({
  ids,
  onClose,
  onSaved,
}: {
  ids: string[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const loader = useCallback(async () => {
    const [folders, details] = await Promise.all([
      api.GET("/api/v1/favorite-folders"),
      Promise.all(ids.map((id) => loadDetail(id, "user"))),
    ]);
    return { folders: result(folders).items, details };
  }, [ids]);
  const data = useResource(loader);
  return (
    <ResourceState resource={data}>
      {(loaded) => (
        <CollectionForm
          key={ids.join(",")}
          ids={ids}
          folders={loaded.folders}
          details={loaded.details}
          onClose={onClose}
          onSaved={onSaved}
        />
      )}
    </ResourceState>
  );
}
function CollectionForm({
  ids,
  folders,
  details,
  onClose,
  onSaved,
}: {
  ids: string[];
  folders: Schema<"FavoriteFolder">[];
  details: Awaited<ReturnType<typeof loadDetail>>[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [checked, setChecked] = useState(
    ids.length === 1 ? details[0].favoriteFolderIds : [],
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function save() {
    setBusy(true);
    try {
      await Promise.all(
        details.map((detail) =>
          toggleFavorite(
            detail.game.id,
            true,
            ids.length === 1
              ? checked
              : [...new Set([...detail.favoriteFolderIds, ...checked])],
          ),
        ),
      );
      onSaved();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "整理失败。");
    } finally {
      setBusy(false);
    }
  }
  return (
    <ConfirmDialog
      open
      title="整理收藏夹"
      description={
        ids.length === 1
          ? "选择这款游戏所属的收藏夹。"
          : `将 ${ids.length} 款游戏加入选中的收藏夹，保留已有分类。`
      }
      confirmLabel="保存分类"
      busy={busy}
      onCancel={onClose}
      onConfirm={() => void save()}
    >
      <div className="favorite-folder-options">
        {folders.map((folder) => (
          <label key={folder.id}>
            <input
              type="checkbox"
              aria-label={folder.name}
              checked={checked.includes(folder.id)}
              onChange={(event) =>
                setChecked((value) =>
                  event.target.checked
                    ? [...value, folder.id]
                    : value.filter((id) => id !== folder.id),
                )
              }
            />
            <span>{folder.name}</span>
            <strong>{folder.gameCount}</strong>
          </label>
        ))}
      </div>
      {!folders.length ? (
        <p className="favorite-folder-none">
          还没有收藏夹，请先从收藏导航新建。
        </p>
      ) : null}
      {error ? <p role="alert">{error}</p> : null}
    </ConfirmDialog>
  );
}
