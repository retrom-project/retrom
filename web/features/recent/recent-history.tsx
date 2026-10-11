"use client";
import { useCallback, useState } from "react";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { PageHeader, EmptyState } from "@/components/ui";
import { ResourceState } from "@/components/resource-state";
import { InfiniteScrollFooter } from "@/components/infinite-scroll-footer";
import { useInfiniteResource } from "@/lib/use-infinite-resource";
import type { PageRequest } from "@/lib/use-infinite-resource";
import { AppIcon } from "@/components/app-icon";
import { RecentCard } from "./recent-card";
import { loadDirectories } from "@/features/library/api";
export function RecentHistory() {
  const [q, setQ] = useState("");
  const [directory, setDirectory] = useState("");
  const [sort, setSort] = useState<"title" | "recent">("recent");
  const [after, setAfter] = useState("");
  const loader = useCallback(
    async ({ offset, limit, signal }: PageRequest) =>
      result(
        await api.GET("/api/v1/recent-games", {
          signal,
          params: {
            query: {
              q,
              platformInstanceId: directory || undefined,
              sort,
              afterMs: after ? new Date(after).getTime() : undefined,
              offset,
              limit,
            },
          },
        }),
      ),
    [q, directory, sort, after],
  );
  const recent = useInfiniteResource(loader, recentKey);
  const directories = useResource(loadDirectories);
  return (
    <>
      <PageHeader
        title="最近游玩"
        description="查看最近打开的游戏与最后游玩时间。"
      />
      <div className="recent-filter-panel recent-query-controls">
        <label className="recent-filter-search">
          搜索游戏
          <span className="recent-input-shell">
            <AppIcon name="search" />
        <input
          value={q}
          aria-label="搜索最近游戏"
          placeholder="搜索游戏…"
          onChange={(event) => {
            setQ(event.target.value);
          }}
        />
          </span>
        </label>
        <label className="recent-filter-select">
          游戏目录
        <select
          value={directory}
          aria-label="游戏目录"
          onChange={(event) => {
            setDirectory(event.target.value);
          }}
        >
          <option value="">全部目录</option>
          {directories.data?.items.map((item) => (
            <option key={item.id} value={item.id}>
              {item.name}
            </option>
          ))}
        </select>
        </label>
        <label className="recent-filter-select">
          排序方式
        <select
          value={sort}
          aria-label="排序"
          onChange={(event) => {
            if (
              event.target.value === "title" ||
              event.target.value === "recent"
            ) {
              setSort(event.target.value);
            }
          }}
        >
          <option value="recent">最近时间</option>
          <option value="title">游戏标题</option>
        </select>
        </label>
        <label className="recent-filter-select">
          最后游玩日期之后
          <input
            type="date"
            value={after}
            onChange={(event) => {
              setAfter(event.target.value);
            }}
          />
        </label>
        <p className="recent-result-count">共 <strong>{recent.data?.total ?? "—"}</strong> 款</p>
      </div>
      {directories.error ? <p role="alert">{directories.error}</p> : null}
      <ResourceState resource={recent}>
        {(data) =>
          data.items.length ? (
            <div className="stack">
              {data.items.map((item) => (
                <RecentCard item={item} key={item.game.id} />
              ))}
              <InfiniteScrollFooter {...recent} count={data.items.length} total={data.total} />
            </div>
          ) : (
            <EmptyState
              title="还没有最近游玩"
              description="开始游戏后，它会出现在这里。"
            />
          )
        }
      </ResourceState>
    </>
  );
}

function recentKey(item: Schema<"RecentPage">["items"][number]) { return item.game.id; }
