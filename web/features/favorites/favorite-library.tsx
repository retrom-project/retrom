"use client";
import { useHorizontalWheel } from "@/lib/use-horizontal-wheel";
import { useToast } from "@/components/toast-provider";
import { useCallback, useState } from "react";
import { useRouter } from "next/navigation";
import { PageHeader, EmptyState } from "@/components/ui";
import { FavoriteFilters } from "./favorite-filters";
import { ResourceState } from "@/components/resource-state";
import { InfiniteScrollFooter } from "@/components/infinite-scroll-footer";
import { useInfiniteResource } from "@/lib/use-infinite-resource";
import type { PageRequest } from "@/lib/use-infinite-resource";
import { ConfirmDialog } from "@/components/confirm-dialog";
import {
  loadGames,
  toggleFavorite,
} from "@/features/library/api";
import type { Game } from "@/lib/api/types";
import type { ListFilters } from "@/features/library/library-browser";
import { FolderManager } from "./folder-manager";
import { FavoriteCard } from "./favorite-card";
import { FavoriteCollectionDialog } from "./favorite-collection-dialog";
export function FavoriteLibrary({ initial }: { initial: ListFilters }) {
  const batchRail = useHorizontalWheel<HTMLDivElement>();
  const [query, setQuery] = useState(initial.q ?? "");
  const [folder, setFolder] = useState(initial.folderId ?? "");
  const [sort, setSort] = useState<"recent" | "title">("recent");
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [organizing, setOrganizing] = useState<string[] | null>(null);
  const [removing, setRemoving] = useState<string[] | null>(null);
  const { notify } = useToast();
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const router = useRouter();
  const loader = useCallback(
    ({ offset, limit, signal }: PageRequest) =>
      loadGames("favorites", {
        q: query,
        folderId: folder && folder !== "unclassified" ? folder : undefined,
        unclassified: folder === "unclassified" || undefined,
        sort,
        offset,
        limit,
      }, signal),
    [query, folder, sort],
  );
  const games = useInfiniteResource(loader, gameKey);
  function changed() {
    games.reload();
    setRevision((value) => value + 1);
    setSelected([]);
    setSelecting(false);
    setOrganizing(null);
    setRemoving(null);
  }
  function filter(next: { q?: string; folder?: string }) {
    const parameters = new URLSearchParams();
    const values = {
      q: next.q ?? query,
      folderId: next.folder ?? folder,
    };
    for (const [key, value] of Object.entries(values)) {
      if (value) {
        parameters.set(key, value);
      }
    }
    router.replace(`/favorites?${parameters}`, { scroll: false });
    setSelected([]);
  }
  async function remove(ids = removing) {
    if (!ids) {
      return;
    }
    setBusy(true);
    try {
      await Promise.all(ids.map((id) => toggleFavorite(id, false)));
      notify({ tone: "good", message: `已取消 ${ids.length} 款游戏的收藏` });
      changed();
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "取消收藏失败，请重试。" });
      games.reload();
    } finally {
      setBusy(false);
    }
  }
  async function clearFolders() {
    setBusy(true);
    try {
      await Promise.all(selected.map((id) => toggleFavorite(id, true, [])));
      notify({ tone: "good", message: `已将 ${selected.length} 款游戏移至未分类` });
      changed();
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "整理失败，请重试。" });
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
        onQuery={(value) => {
          setQuery(value);
          filter({ q: value });
        }}
        onSort={(value) => {
          setSort(value);
          setSelected([]);
        }}
        onToggle={() => {
          setSelecting(!selecting);
          setSelected([]);
        }}
      />
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
              <InfiniteScrollFooter {...games} count={data.items.length} total={data.total} />
              {selecting ? (
                <div ref={batchRail} className="favorite-batch">
                  <strong>已选 {selected.length} 款</strong>
                  <button
                    onClick={() =>
                      setSelected(data.items.map((game) => game.id))
                    }
                  >
                    全选已加载
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
      </ConfirmDialog>
    </div>
  );
}

function gameKey(game: Game) { return game.id; }
