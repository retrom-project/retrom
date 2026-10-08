"use client";
import { useRouter } from "next/navigation";
import { GameDetailContent } from "./game-detail-content";
import { useCallback, useState } from "react";
import { api, result, ApiError } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { loadDetail, toggleFavorite } from "@/features/library/api";
import { ResourceState } from "@/components/resource-state";
import { AdminGameDetail } from "./admin-game-detail";
import { useToast } from "@/components/toast-provider";
export function GameDetail({
  gameId,
  mode = "user",
}: {
  gameId: string;
  mode?: "user" | "admin" | "review";
}) {
  const router = useRouter();
  const loader = useCallback(() => loadDetail(gameId, mode), [gameId, mode]);
  const detail = useResource(loader);
  const [busy, setBusy] = useState(false);
  const { notify } = useToast();
  async function nextReview() {
    let nextId: string | undefined;
    try {
      const pending = result(await api.GET("/api/v1/admin/reviews", {
        params: { query: { offset: 0, limit: 1 } },
      }));
      nextId = pending.items[0]?.id;
    } catch {
      notify({ tone: "warn", message: "游戏已发布，但无法读取下一条待审核游戏，已返回待审核列表。" });
    }
    router.replace(nextId ? `/admin/reviews/${nextId}` : "/admin/reviews");
  }
  async function review(action: "approve" | "discard") {
    if (!detail.data || busy) {
      return;
    }
    setBusy(true);
    const params = { path: { gameId } };
    const body = { version: detail.data.game.version };
    try {
      if (action === "approve") {
        result(
          await api.POST("/api/v1/admin/reviews/{gameId}/approve", {
            params,
            body,
          }),
        );
        notify({ tone: "good", message: "游戏已通过审核并发布" });
        await nextReview();
      } else {
        const response = await api.POST(
          "/api/v1/admin/reviews/{gameId}/discard",
          { params, body },
        );
        if (response.error) {
          throw new ApiError(response.error.code, response.error.message, response.response.status);
        }
        notify({ tone: "good", message: "审核条目已丢弃" });
        router.push("/admin/reviews");
      }
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "审核操作失败，请重试。" });
      detail.reload();
    } finally {
      setBusy(false);
    }
  }
  async function favorite() {
    if (!detail.data || busy) {
      return;
    }
    setBusy(true);
    try {
      await toggleFavorite(gameId, !detail.data.game.favorite);
      notify({ tone: "good", message: detail.data.game.favorite ? "已取消收藏" : "已收藏游戏" });
      detail.reload();
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "收藏操作失败，请重试。" });
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="page-layout page-layout-detail game-detail-page">
      <ResourceState resource={detail}>
        {(data) => (
          mode === "user" ? <GameDetailContent
            detail={data}
            mode={mode}
            onFavorite={() => void favorite()}
            onReview={(action) => void review(action)}
            onChange={detail.reload}
            busy={busy}
          /> : <AdminGameDetail
            key={data.game.id}
            detail={data}
            mode={mode}
            onReview={(action) => void review(action)}
            onChange={detail.reload}
            busy={busy}
          />
        )}
      </ResourceState>
    </div>
  );
}
