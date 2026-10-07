"use client";
import { useRouter } from "next/navigation";
import { GameDetailContent } from "./game-detail-content";
import { useCallback, useState } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { loadDetail, toggleFavorite } from "@/features/library/api";
import { ResourceState } from "@/components/resource-state";
import { GameEditor } from "./game-editor";
import { GameManagement } from "./game-management";
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
  const [error, setError] = useState("");
  async function review(action: "approve" | "discard") {
    if (!detail.data) {
      return;
    }
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
        router.push(`/games/${gameId}`);
      } else {
        const response = await api.POST(
          "/api/v1/admin/reviews/{gameId}/discard",
          { params, body },
        );
        if (response.error) {
          throw new Error(response.error.message);
        }
        router.push("/admin/reviews");
      }
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "操作失败。");
      detail.reload();
    }
  }
  async function favorite() {
    if (!detail.data) {
      return;
    }
    try {
      await toggleFavorite(gameId, !detail.data.game.favorite);
      detail.reload();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "操作失败。");
    }
  }
  return (
    <div className="page-layout page-layout-detail game-detail-page">
      <ResourceState resource={detail}>
        {(data) => (
          <>
            <GameDetailContent
              detail={data}
              mode={mode}
              onFavorite={() => void favorite()}
              onReview={(action) => void review(action)}
              onChange={detail.reload}
              error={error}
            />
            {mode !== "user" ? (
              <div className="workspace-section stack">
                <GameEditor
                  key={data.game.version}
                  detail={data}
                  mode={mode}
                  onSaved={detail.reload}
                />
                <GameManagement detail={data} onChange={detail.reload} />
              </div>
            ) : null}
          </>
        )}
      </ResourceState>
    </div>
  );
}
