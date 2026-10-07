"use client";
import { DraftRecovery } from "@/features/player/draft-recovery";
import { useCallback, useState } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { PageHeader, EmptyState } from "@/components/ui";
import { ResourceState } from "@/components/resource-state";
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
  const [offset, setOffset] = useState(0);
  const loader = useCallback(
    async () =>
      result(
        await api.GET("/api/v1/saves", {
          params: {
            query: {
              ...filters,
              gameId: filters.gameId || undefined,
              kind: filters.kind || undefined,
              offset,
              limit: 24,
            },
          },
        }),
      ),
    [filters, offset],
  );
  const saves = useResource(loader);
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
          setOffset(0);
        }}
      />
      <ResourceState resource={saves}>
        {(data) =>
          data.items.length ? (
            <div className="stack">
              <SaveGroups saves={data.items} onChange={saves.reload} />
              <div className="workspace-paging">
                <span>共 {data.total} 份</span>
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
