"use client";
import { useEffect, useRef, useState } from "react";
import { useToast } from "@/components/toast-provider";
import { api, result } from "@/lib/api/client";
import { createRecommendedTags } from "./recommended-tags";

export function RecommendedTagsButton({ onCreated }: { onCreated: () => void }) {
  const { notify } = useToast();
  const running = useRef<AbortController | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => () => running.current?.abort(), []);
  async function create() {
    if (running.current) { return; }
    const controller = new AbortController();
    running.current = controller;
    setBusy(true);
    try {
      const summary = await createRecommendedTags({
        list: async (offset) => result(await api.GET("/api/v1/admin/tags", { params: { query: { offset, limit: 100 } }, signal: controller.signal })),
        create: async (name) => result(await api.POST("/api/v1/admin/tags", { body: { name } })),
      }, controller.signal);
      if (!controller.signal.aborted) {
        notify({ tone: summary.failures.length ? "warn" : "good", message: `推荐标签：已创建 ${summary.created} 个，已存在 ${summary.existing} 个，失败 ${summary.failures.length} 个。${summary.failures.length ? "再次点击可重试未创建项。" : ""}` });
        onCreated();
      }
    } catch (failure) {
      if (!controller.signal.aborted) { notify({ tone: "bad", message: failure instanceof Error ? failure.message : "无法创建推荐标签。" }); }
    } finally {
      if (!controller.signal.aborted) { setBusy(false); }
      running.current = null;
    }
  }
  return <button className="button secondary" disabled={busy} onClick={() => void create()}>{busy ? "正在创建推荐标签…" : "一键创建推荐标签"}</button>;
}
