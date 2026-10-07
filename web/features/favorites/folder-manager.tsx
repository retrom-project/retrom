"use client";
import { useToast } from "@/components/toast-provider";
import { useEffect, useState } from "react";
import { api, result, ApiError } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { usePhoneLayout } from "@/lib/use-phone-layout";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import { FloatingFolderPanel } from "./floating-folder-panel";
async function loadFolders() {
  const [folders, all, unclassified] = await Promise.all([
    api.GET("/api/v1/favorite-folders"),
    api.GET("/api/v1/favorites", { params: { query: { limit: 1 } } }),
    api.GET("/api/v1/favorites", {
      params: { query: { limit: 1, unclassified: true } },
    }),
  ]);
  return {
    folders: result(folders).items,
    total: result(all).total,
    unclassified: result(unclassified).total,
  };
}
export function FolderManager({
  selected,
  onSelect,
  onChange,
  revision = 0,
}: {
  selected: string;
  onSelect: (id: string) => void;
  onChange: () => void;
  revision?: number;
}) {
  const resource = useResource(loadFolders);
  const { reload } = resource;
  useEffect(() => {
    reload();
  }, [revision, reload]);
  const phone = usePhoneLayout();
  const [sheet, setSheet] = useState(false);
  const [editing, setEditing] = useState<
    Schema<"FavoriteFolder"> | "new" | null
  >(null);
  const [deleting, setDeleting] = useState<Schema<"FavoriteFolder"> | null>(
    null,
  );
  const [name, setName] = useState("");
  const { notify } = useToast();
  const [busy, setBusy] = useState(false);
  const current = resource.data?.folders.find(
    (folder) => folder.id === selected,
  );
  function edit(folder: Schema<"FavoriteFolder"> | "new") {
    setName(folder === "new" ? "" : folder.name);
    setEditing(folder);
  }
  async function save() {
    if (!editing) {
      return;
    }
    setBusy(true);
    try {
      if (editing === "new") {
        result(
          await api.POST("/api/v1/favorite-folders", {
            body: { name: name.trim() },
          }),
        );
      } else {
        result(
          await api.PATCH("/api/v1/favorite-folders/{folderId}", {
            params: { path: { folderId: editing.id } },
            body: { name: name.trim(), version: editing.version },
          }),
        );
      }
      notify({ tone: "good", message: editing === "new" ? "收藏夹已创建" : "收藏夹名称已更新" });
      setEditing(null);
      resource.reload();
      onChange();
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "保存失败，请重试。" });
    } finally {
      setBusy(false);
    }
  }
  async function remove() {
    if (!deleting) {
      return;
    }
    setBusy(true);
    try {
      const response = await api.DELETE("/api/v1/favorite-folders/{folderId}", {
        params: { path: { folderId: deleting.id } },
      });
      if (response.error) { throw new ApiError(response.error.code, response.error.message, response.response.status); }
      notify({ tone: "good", message: "收藏夹已删除，游戏收藏已保留" });
      setDeleting(null);
      onSelect("");
      resource.reload();
      onChange();
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "删除收藏夹失败，请重试。" });
    } finally {
      setBusy(false);
    }
  }
  function select(id: string) {
    onSelect(id);
    setSheet(false);
  }
  const navigation = (
    <FolderNavigation
      data={resource.data}
      error={resource.error}
      selected={selected}
      current={current}
      onSelect={select}
      onEdit={edit}
      onDelete={(folder) => {
            setDeleting(folder);
      }}
    />
  );
  return (
    <>
      {phone ? (
        <>
          <button className="button secondary" onClick={() => setSheet(true)}>
            <AppIcon name="heart" />
            {current?.name ?? (selected ? "未分类" : "全部收藏")}
          </button>
          <ResponsiveSheet
            open={sheet}
            title="收藏导航"
            placement="bottom"
            onClose={() => setSheet(false)}
          >
            <aside className="favorite-rail">{navigation}</aside>
          </ResponsiveSheet>
        </>
      ) : (
        <FloatingFolderPanel>{navigation}</FloatingFolderPanel>
      )}
      <ConfirmDialog
        open={!!editing}
        title={editing === "new" ? "新建收藏夹" : "收藏夹改名"}
        confirmLabel="保存"
        confirmDisabled={!name.trim()}
        busy={busy}
        onCancel={() => setEditing(null)}
        onConfirm={() => void save()}
      >
        <label className="field">
          收藏夹名称
          <input
            value={name}
            maxLength={120}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
      </ConfirmDialog>
      <ConfirmDialog
        open={!!deleting}
        title="删除收藏夹"
        description="收藏的游戏会保留，只有此收藏夹及其分类关系被删除。"
        tone="danger"
        busy={busy}
        onCancel={() => setDeleting(null)}
        onConfirm={() => void remove()}
      >
      </ConfirmDialog>
    </>
  );
}

function FolderNavigation({
  data,
  error,
  selected,
  current,
  onSelect,
  onEdit,
  onDelete,
}: {
  data: Awaited<ReturnType<typeof loadFolders>> | null;
  error: string;
  selected: string;
  current: Schema<"FavoriteFolder"> | undefined;
  onSelect: (id: string) => void;
  onEdit: (folder: Schema<"FavoriteFolder"> | "new") => void;
  onDelete: (folder: Schema<"FavoriteFolder">) => void;
}) {
  return (
    <div className="favorite-navigation-body">
      <nav aria-label="收藏夹导航">
        <button
          className={!selected ? "is-active" : ""}
          onClick={() => onSelect("")}
        >
          <span>♥</span>
          <span>全部收藏</span>
          <strong>{data?.total ?? "—"}</strong>
        </button>
        <button
          className={selected === "unclassified" ? "is-active" : ""}
          onClick={() => onSelect("unclassified")}
        >
          <span>○</span>
          <span>未分类</span>
          <strong>{data?.unclassified ?? "—"}</strong>
        </button>
        <p className="favorite-rail-label">收藏夹</p>
        {data?.folders.map((folder) => (
          <button
            key={folder.id}
            className={selected === folder.id ? "is-active" : ""}
            onClick={() => onSelect(folder.id)}
          >
            <span aria-hidden="true">▣</span>
            <span>{folder.name}</span>
            <strong>{folder.gameCount}</strong>
          </button>
        ))}
      </nav>
      {current ? (
        <div className="workspace-actions">
          <button
            className="favorite-edit-folder"
            onClick={() => onEdit(current)}
          >
            改名
          </button>
          <button
            className="favorite-edit-folder"
            onClick={() => {
              onDelete(current);
            }}
          >
            删除收藏夹
          </button>
        </div>
      ) : null}
      {error ? <p role="alert">{error}</p> : null}
      <button className="favorite-new-folder" onClick={() => onEdit("new")}>
        ＋ 新建收藏夹
      </button>
    </div>
  );
}
