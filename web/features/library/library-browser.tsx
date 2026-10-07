"use client";
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
import { FolderManager } from "@/features/favorites/folder-manager";
export type LibraryKind = "library" | "favorites" | "admin" | "review";
export function LibraryBrowser({
  kind = "library",
  initial = {},
}: {
  kind?: LibraryKind;
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
        description={
          kind === "review"
            ? "所有来源的待审游戏在这里完成首次发布。"
            : "浏览你的复古游戏资料库。"
        }
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
      <ResourceState resource={games}>
        {(data) =>
          data.items.length ? (
            <>
              <div className="library-game-grid">
                {data.items.map((game) => (
                  <GameCard
                    key={game.id}
                    game={game}
                    href={
                      kind === "review"
                        ? `/admin/reviews/${game.id}`
                        : kind === "admin"
                          ? `/admin/games/${game.id}`
                          : undefined
                    }
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
              title={kind === "review" ? "没有待审游戏" : "没有找到游戏"}
              description={
                kind === "review"
                  ? "从服务端来源扫描接收的游戏会显示在这里。"
                  : "调整搜索与筛选条件后再试。"
              }
            />
          )
        }
      </ResourceState>
    </div>
  );
}
