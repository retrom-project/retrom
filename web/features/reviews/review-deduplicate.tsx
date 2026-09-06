"use client";

import { useRef, useState } from "react";
import { useAuth } from "@/features/auth/auth-provider";
import { api, writeHeaders } from "@/lib/api/client";
import type { components } from "@/lib/api/generated/schema";
import { newUuid } from "@/lib/crypto";
import { responseError } from "@/lib/upload";
import { refreshReviewQueue } from "./review-queue-refresh";

type Request = components["schemas"]["ReviewDeduplicateRequest"];
const scopeKeys = ["q", "tagId", "importJobId", "pegasusImportId", "emulationStationImportId", "platformInstanceId", "blockerCode"];

export function ReviewDeduplicate({ values }: { values: Record<string, string> }) {
  const { context } = useAuth();
  const [busy, setBusy] = useState(false);
  const running = useRef(false);

  async function deduplicate() {
    if (running.current) { return; }
    running.current = true;
    setBusy(true);
    const scope: Request["scope"] = Object.fromEntries(Object.entries(values).filter(([key, value]) => scopeKeys.includes(key) && value));
    let body: Request = { scope };
    let discarded = 0;
    let skipped = 0;
    try {
      for (;;) {
        const { data, response } = await api.POST("/api/v1/admin/reviews/deduplicate", {
          params: { header: { ...writeHeaders(), "Idempotency-Key": newUuid() } }, body,
        });
        if (!data) { throw new Error(await responseError(response, "去重未完成，请重试")); }
        discarded += data.discardedCount;
        skipped += data.attachmentActiveCount;
        if (!data.nextAfterItemId) { break; }
        body = { scope, afterItemId: data.nextAfterItemId, throughItemId: data.throughItemId ?? undefined };
      }
      refreshReviewQueue(context.user?.userId, {
        tone: skipped ? "warn" : "good",
        message: `去重完成，已丢弃 ${discarded} 个与已发布游戏重复的条目。${skipped ? `另有 ${skipped} 个条目正在补传，已跳过。` : ""}`,
      });
    } catch (caught) {
      const message = caught instanceof Error ? caught.message : "去重未完成，请重试";
      refreshReviewQueue(context.user?.userId, { tone: "bad", message: `${message}；已确认丢弃 ${discarded} 个条目，可再次点击快速去重继续处理。` });
    } finally {
      running.current = false;
      setBusy(false);
    }
  }

  return <button type="button" className="button secondary" disabled={busy}
    title="自动丢弃当前筛选范围内与已发布游戏内容相同的待审核条目"
    onClick={() => void deduplicate()}>{busy ? "正在去重…" : "快速去重"}</button>;
}
