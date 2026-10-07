"use client";
import { useEffect, useRef, useState } from "react";
import { useToast } from "@/components/toast-provider";
import { api, result } from "@/lib/api/client";
import type { GameQuery } from "@/features/library/api";
import { approveReviewSnapshot, reviewSnapshot } from "./review-approval";
import type { ReviewApprovalSummary } from "./review-approval";

export function useReviewApproval(query: GameQuery, onComplete: () => void) {
  const { notify } = useToast();
  const running = useRef<AbortController | null>(null);
  const [busy, setBusy] = useState(false);
  const [summary, setSummary] = useState<ReviewApprovalSummary | null>(null);
  useEffect(() => () => running.current?.abort(), []);

  async function start() {
    if (running.current) { return; }
    const controller = new AbortController();
    running.current = controller;
    setBusy(true);
    setSummary(null);
    try {
      const snapshot = await reviewSnapshot(async (offset) => result(await api.GET("/api/v1/admin/reviews", {
        params: { query: { ...query, offset, limit: 100 } }, signal: controller.signal,
      })), controller.signal);
      const completed = await approveReviewSnapshot(snapshot, {
        readiness: async (games) => result(await api.POST("/api/v1/admin/reviews/readiness", {
          body: { gameIds: games.map((game) => game.id) }, signal: controller.signal,
        })).items,
        approve: async (game) => {
          result(await api.POST("/api/v1/admin/reviews/{gameId}/approve", {
            params: { path: { gameId: game.id } }, body: { version: game.version },
          }));
        },
      }, controller.signal, (value) => { if (!controller.signal.aborted) { setSummary(value); } });
      if (!controller.signal.aborted) {
        setSummary(completed);
        notify({
          tone: completed.failed || completed.interrupted || completed.missingBios ? "warn" : "good",
          message: `快速审批${completed.interrupted ? "已停止" : "完成"}：已发布 ${completed.approved} 项，缺少 BIOS ${completed.missingBios} 项，失败 ${completed.failed} 项。`,
        });
        onComplete();
      }
    } catch (failure) {
      if (!controller.signal.aborted) {
        notify({ tone: "bad", message: failure instanceof Error ? failure.message : "无法读取待审游戏，请重试。" });
      }
    } finally {
      if (!controller.signal.aborted) { setBusy(false); }
      if (running.current === controller) { running.current = null; }
    }
  }
  return { busy, summary, start };
}
