"use client";
import type { Schema } from "@/lib/api/types";
import { useCallback, useState } from "react";
import { PageHeader, EmptyState } from "@/components/ui";
import { AppIcon } from "@/components/app-icon";
import { ResourceState } from "@/components/resource-state";
import { useResource } from "@/lib/use-resource";
import { loadCatalog, loadDirectories, loadGames, loadTags } from "./api";
import { GameCard } from "./game-card";
import { LibraryPlatformFilter } from "./library-platform-filter";
import { LibraryFilterFields } from "./library-filter-fields";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import { useLibraryQuery } from "./use-library-query";
import type { ListFilters } from "./use-library-query";
export type { ListFilters } from "./use-library-query";
import { AdminGameBrowser } from "@/features/games/admin-game-browser";
import { FolderManager } from "@/features/favorites/folder-manager";
export type LibraryKind = "library" | "favorites" | "admin" | "review";
export function LibraryBrowser({ kind = "library", initial = {} }: { kind?: LibraryKind; initial?: ListFilters }) {
  return kind === "admin" || kind === "review"
    ? <AdminGameBrowser kind={kind} initial={initial} />
    : <PublicLibraryBrowser kind={kind} initial={initial} />;
}
function PublicLibraryBrowser({
  kind = "library",
  initial = {},
}: {
  kind?: "library" | "favorites";
  initial?: ListFilters;
}) {
  const { values, query, update } = useLibraryQuery(initial);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const { q, directory, platform, tag, folder, sort, offset } = values;
  const loader = useCallback(() => loadGames(kind, query), [kind, query]);
  const games = useResource(loader);
  const directories = useResource(loadDirectories);
  const catalog = useResource(loadCatalog);
  const tags = useResource(loadTags);
  const filterError = [directories.error, tags.error, catalog.error].find(
    Boolean,
  );
  const filters = (
    <LibraryFilterFields
      directory={directory}
      tag={tag}
      sort={sort}
      directories={directories.data?.items ?? []}
      tags={tags.data?.items ?? []}
      onDirectory={(value) => update({ directory: value })}
      onTag={(value) => update({ tag: value })}
      onSort={(value) => update({ sort: value })}
    />
  );
  const title = {
    library: "游戏库",
    favorites: "我的收藏",
    admin: "游戏管理",
    review: "待审核",
  }[kind];
  return (
    <div className="page-layout-library">
      <PageHeader
        title={title}
        description="浏览你的复古游戏资料库。"
      />
      {kind === "favorites" ? (
        <FolderManager
          selected={folder}
          onSelect={(value) => {
            update({ folder: value });
          }}
          onChange={games.reload}
        />
      ) : null}
      <div className="library-toolbar">
        <div className="library-tool-row">
          <label className="library-search">
            <AppIcon name="search" />
            <input
              value={q}
              aria-label="搜索游戏"
              placeholder="搜索游戏名称…"
              onChange={(event) => {
                update({ q: event.target.value });
              }}
            />
          </label>
          <div className="library-desktop-filter">{filters}</div>
          <button
            className="button secondary library-mobile-filter-trigger"
            aria-label="筛选游戏"
            aria-expanded={filtersOpen}
            onClick={() => setFiltersOpen(true)}
          >
            <AppIcon name="filter" />
          </button>
        </div>
        <LibraryPlatformFilter
          platforms={catalog.data?.platforms ?? []}
          directories={directories.data?.items ?? []}
          selected={platform}
          onSelect={(value) => {
            update({ platform: value });
          }}
        />
        {filterError ? <p role="alert">{filterError}</p> : null}
      </div>
      <ResponsiveSheet
        open={filtersOpen}
        title="筛选游戏"
        onClose={() => setFiltersOpen(false)}
      >
        <div className="library-filter-sheet">{filters}</div>
        <button className="button" onClick={() => setFiltersOpen(false)}>
          完成筛选
        </button>
      </ResponsiveSheet>
      <LibrarySectionHeader data={games.data} />
      <ResourceState resource={games}>
        {(data) =>
          data.items.length ? (
            <>
              <div className="library-game-grid">
                {data.items.map((game) => (
                  <GameCard
                    key={game.id}
                    game={game}
                    onChange={games.reload}
                  />
                ))}
              </div>
              <div className="workspace-paging">
                <span>共 {data.total} 款</span>
                <button
                  className="button secondary"
                  disabled={offset === 0}
                  onClick={() =>
                    update({ offset: Math.max(0, offset - 24) }, false)
                  }
                >
                  上一页
                </button>
                <button
                  className="button secondary"
                  disabled={offset + 24 >= data.total}
                  onClick={() => update({ offset: offset + 24 }, false)}
                >
                  下一页
                </button>
              </div>
            </>
          ) : (
            <EmptyState
              title="没有找到游戏"
              description="调整搜索与筛选条件后再试。"
            />
          )
        }
      </ResourceState>
    </div>
  );
}

function LibrarySectionHeader({ data }: { data: Schema<"GamePage"> | null }) {
  return <div className="library-section-head">
    <div><h2>所有游戏</h2><p>按平台、目录和标签找到想玩的游戏。</p></div>
    <span>已加载 {data?.items.length ?? 0} / {data?.total ?? 0} 款游戏</span>
  </div>;
}
