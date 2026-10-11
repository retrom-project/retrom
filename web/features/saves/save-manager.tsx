"use client";
import { DraftRecovery } from "@/features/player/draft-recovery";
import { useCallback, useState } from "react";
import { api, result } from "@/lib/api/client";
import type { Save } from "@/lib/api/types";
import { PageHeader, EmptyState } from "@/components/ui";
import { ResourceState } from "@/components/resource-state";
import { InfiniteScrollFooter } from "@/components/infinite-scroll-footer";
import { useInfiniteResource } from "@/lib/use-infinite-resource";
import type { PageRequest } from "@/lib/use-infinite-resource";
import { SaveGroups } from "./save-groups";
import { SaveFeatured } from "./save-featured";
import { SaveFilters } from "./save-filters";
import type { SaveFiltersValue } from "./save-filters";
export function SaveManager({ gameId = "" }: { gameId?: string }) {
  const [filters, setFilters] = useState<SaveFiltersValue>({
    q: "",
    gameId,
    kind: "",
    sort: "recent",
  });
  const loader = useCallback(
    async ({ offset, limit, signal }: PageRequest) =>
      result(
        await api.GET("/api/v1/saves", {
          signal,
          params: {
            query: {
              ...filters,
              gameId: filters.gameId || undefined,
              kind: filters.kind || undefined,
              offset,
              limit,
            },
          },
        }),
      ),
    [filters],
  );
  const saves = useInfiniteResource(loader, saveKey);
  return (
    <>
      <DraftRecovery />
      <PageHeader
        title="我的存档"
        description="查看保存画面，找到想恢复的游戏状态，并随时从这里继续。"
        actions={
          <div className="save-head-summary">
            <div>
              <span>存档</span>
              <strong>{saves.data?.total ?? "—"} 份</strong>
            </div>
            <div>
              <span>涉及游戏</span>
              <strong>{saves.data?.gameCount ?? "—"} 款</strong>
            </div>
          </div>
        }
      />
      {saves.data ? (
        <SaveFeatured save={saves.data.items.find((save) => save.restorable)} />
      ) : null}
      <SaveFilters
        value={filters}
        total={saves.data?.total}
        onChange={(value) => {
          setFilters(value);
        }}
      />
      <ResourceState resource={saves}>
        {(data) =>
          data.items.length ? (
            <div className="stack">
              <SaveGroups saves={data.items} onChange={saves.reload} />
              <InfiniteScrollFooter {...saves} count={data.items.length} total={data.total} />
            </div>
          ) : (
            <EmptyState
              title="还没有存档"
              description="开始游戏后，可以保存即时进度或同步游戏内存档。"
            />
          )
        }
      </ResourceState>
    </>
  );
}

function saveKey(save: Save) { return save.id; }
