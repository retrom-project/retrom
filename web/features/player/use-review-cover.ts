"use client";
import { useState } from "react";
import { useToast } from "@/components/toast-provider";
import { api, result, upload } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import type { PlayerRuntimeV1 } from "./runtime/contract";
import { screenshotFileName } from "./screenshot-file";
export function useReviewCover(run: Schema<"Run">) {
  const { notify } = useToast();
  const [busy, setBusy] = useState(false);
  async function save(runtime: PlayerRuntimeV1 | null) {
    if (!runtime || run.purpose !== "review") {
      return;
    }
    setBusy(true);
    try {
      const screenshot = await runtime.screenshot();
      const detail = result(
        await api.GET("/api/v1/admin/reviews/{gameId}", {
          params: { path: { gameId: run.gameId } },
        }),
      );
      const body = new FormData();
      body.set(
        "file",
        screenshot,
        screenshotFileName("review-cover", screenshot),
      );
      body.set("kind", "cover");
      body.set("version", String(detail.game.version));
      await upload<Schema<"GameDetail">>(
        `/api/v1/admin/games/${run.gameId}/media`,
        body,
      );
      notify({ tone: "good", message: "当前画面已选为游戏封面。" });
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "无法选用截图。" });
    } finally {
      setBusy(false);
    }
  }
  return { save, busy };
}
