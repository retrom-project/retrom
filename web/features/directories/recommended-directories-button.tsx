"use client";
import { useEffect, useRef, useState } from "react";
import { useToast } from "@/components/toast-provider";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { createRecommendedDirectories, recommendedDirectories } from "./recommended-directories";

export function RecommendedDirectoriesButton({ catalog, onCreated }: { catalog: Schema<"RuntimeCatalog"> | null; onCreated: () => void }) {
  const { notify } = useToast();
  const running = useRef<AbortController | null>(null);
  const [busy, setBusy] = useState(false);
  const count = catalog ? recommendedDirectories(catalog).length : 0;
  useEffect(() => () => running.current?.abort(), []);
  async function create() {
    if (running.current || !catalog) { return; }
    const controller = new AbortController();
    running.current = controller;
    setBusy(true);
    try {
      // Refresh declarations when clicked; the page may have been open across a Provider change.
      const current = result(await api.GET("/api/v1/runtime/catalog", { signal: controller.signal }));
      const summary = await createRecommendedDirectories(recommendedDirectories(current), {
        list: async () => result(await api.GET("/api/v1/admin/platform-instances", { signal: controller.signal })).items,
        create: async (body) => result(await api.POST("/api/v1/admin/platform-instances", { body })),
      }, controller.signal);
      if (!controller.signal.aborted) {
        notify({ tone: summary.failures.length ? "warn" : "good", message: `推荐目录：已创建 ${summary.created} 个，已存在 ${summary.existing} 个，失败 ${summary.failures.length} 个。${summary.failures.length ? "再次点击可重试未创建项。" : ""}` });
        onCreated();
      }
    } catch (failure) {
      if (!controller.signal.aborted) { notify({ tone: "bad", message: failure instanceof Error ? failure.message : "无法创建推荐目录。" }); }
    } finally {
      if (!controller.signal.aborted) { setBusy(false); }
      running.current = null;
    }
  }
  return <button className="button secondary" disabled={busy || !count} title="仅创建当前已安装核心支持的推荐目录，保留已有配置" onClick={() => void create()}>{busy ? "正在创建推荐目录…" : "一键创建推荐目录"}</button>;
}
