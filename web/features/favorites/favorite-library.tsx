"use client";
import { useCallback, useState } from "react";
import { useRouter } from "next/navigation";
import { PageHeader, EmptyState } from "@/components/ui";
import { FavoriteFilters } from "./favorite-filters";
import { ResourceState } from "@/components/resource-state";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { useResource } from "@/lib/use-resource";
import {
  loadDirectories,
  loadGames,
  toggleFavorite,
} from "@/features/library/api";
import type { ListFilters } from "@/features/library/library-browser";
import { FolderManager } from "./folder-manager";
import { FavoriteCard } from "./favorite-card";
import { FavoriteCollectionDialog } from "./favorite-collection-dialog";
export function FavoriteLibrary({ initial }: { initial: ListFilters }) {
  const [query, setQuery] = useState(initial.q ?? "");
  const [directory, setDirectory] = useState(initial.platformInstanceId ?? "");
  const [folder, setFolder] = useState(initial.folderId ?? "");
  const [sort, setSort] = useState<"recent" | "title">("recent");
  const [offset, setOffset] = useState(0);
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [organizing, setOrganizing] = useState<string[] | null>(null);
  const [removing, setRemoving] = useState<string[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const router = useRouter();
  const loader = useCallback(
    () =>
      loadGames("favorites", {
        q: query,
        platformInstanceId: directory || undefined,
        folderId: folder && folder !== "unclassified" ? folder : undefined,
        unclassified: folder === "unclassified" || undefined,
        sort,
        offset,
        limit: 24,
      }),
    [query, directory, folder, sort, offset],
  );
  const games = useResource(loader);
  const directories = useResource(loadDirectories);
  function changed() {
    games.reload();
    setRevision((value) => value + 1);
    setSelected([]);
    setSelecting(false);
    setOrganizing(null);
    setRemoving(null);
  }
  function filter(next: { q?: string; directory?: string; folder?: string }) {
    const parameters = new URLSearchParams();
    const values = {
      q: next.q ?? query,
      platformInstanceId: next.directory ?? directory,
      folderId: next.folder ?? folder,
    };
    for (const [key, value] of Object.entries(values)) {
      if (value) {
        parameters.set(key, value);
      }
    }
    router.replace(`/favorites?${parameters}`, { scroll: false });
    setOffset(0);
    setSelected([]);
  }
  async function remove(ids = removing) {
    if (!ids) {
      return;
    }
    setBusy(true);
    try {
      await Promise.all(ids.map((id) => toggleFavorite(id, false)));
      changed();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "取消收藏失败。");
      games.reload();
    } finally {
      setBusy(false);
    }
  }
  async function clearFolders() {
    setBusy(true);
    setError("");
    try {
      await Promise.all(selected.map((id) => toggleFavorite(id, true, [])));
      changed();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "整理失败。");
      games.reload();
    } finally {
      setBusy(false);
    }
  }
  function choose(id: string) {
    setSelected((values) =>
      values.includes(id)
        ? values.filter((value) => value !== id)
        : [...values, id],
    );
  }
  return (
    <div className="favorite-page">
      <PageHeader
        title="我的收藏"
        description="整理喜欢的游戏，按收藏夹分类，随时找到下一款想玩的游戏。"
        actions={
          <p className="favorite-head-summary">
            <strong>{games.data?.total ?? "—"}</strong> 款收藏
          </p>
        }
      />
      <FolderManager
        selected={folder}
        revision={revision}
        onSelect={(value) => {
          setFolder(value);
          filter({ folder: value });
        }}
        onChange={games.reload}
      />
      <FavoriteFilters
        query={query}
        sort={sort}
        selecting={selecting}
        directory={directory}
        directories={directories.data?.items ?? []}
        onQuery={(value) => {
          setQuery(value);
          filter({ q: value });
        }}
        onSort={(value) => {
          setSort(value);
          setOffset(0);
        }}
        onDirectory={(value) => {
          setDirectory(value);
          filter({ directory: value });
        }}
        onToggle={() => {
          setSelecting(!selecting);
          setSelected([]);
        }}
      />
      {error || directories.error ? (
        <p role="alert">{error || directories.error}</p>
      ) : null}
      <ResourceState resource={games}>
        {(data) =>
          data.items.length ? (
            <>
              <div
                className={`favorite-game-grid${selecting ? " is-selecting" : ""}`}
              >
                {data.items.map((game) => (
                  <FavoriteCard
                    key={game.id}
                    game={game}
                    selecting={selecting}
                    selected={selected.includes(game.id)}
                    onSelect={() => choose(game.id)}
                    onOrganize={() => setOrganizing([game.id])}
                    onRemove={() => void remove([game.id])}
                  />
                ))}
              </div>
              <div className="workspace-paging">
                <span>共 {data.total} 款</span>
                <button
                  className="button secondary"
                  disabled={offset === 0}
                  onClick={() => setOffset(Math.max(0, offset - 24))}
                >
                  上一页
                </button>
                <button
                  className="button secondary"
                  disabled={offset + 24 >= data.total}
                  onClick={() => setOffset(offset + 24)}
                >
                  下一页
                </button>
              </div>
              {selecting ? (
                <div className="favorite-batch">
                  <strong>已选 {selected.length} 款</strong>
                  <button
                    onClick={() =>
                      setSelected(data.items.map((game) => game.id))
                    }
                  >
                    全选本页
                  </button>
                  <button
                    disabled={!selected.length || busy}
                    className="is-primary"
                    onClick={() => setOrganizing(selected)}
                  >
                    加入收藏夹
                  </button>
                  <button
                    disabled={!selected.length || busy}
                    onClick={() => void clearFolders()}
                  >
                    设为未分类
                  </button>
                  <button
                    disabled={!selected.length || busy}
                    className="is-danger"
                    onClick={() => {
                      setError("");
                      setRemoving(selected);
                    }}
                  >
                    取消收藏
                  </button>
                </div>
              ) : null}
            </>
          ) : (
            <EmptyState
              title="还没有收藏游戏"
              description="点击游戏卡片上的爱心，收藏喜欢的游戏。"
            />
          )
        }
      </ResourceState>
      {organizing ? (
        <FavoriteCollectionDialog
          ids={organizing}
          onClose={() => setOrganizing(null)}
          onSaved={changed}
        />
      ) : null}
      <ConfirmDialog
        open={!!removing}
        title="取消收藏"
        description={`确认取消收藏这 ${removing?.length ?? 0} 款游戏？`}
        confirmLabel="取消收藏"
        tone="danger"
        busy={busy}
        onCancel={() => setRemoving(null)}
        onConfirm={() => void remove()}
      >
        {error ? <p role="alert">{error}</p> : null}
      </ConfirmDialog>
    </div>
  );
}
