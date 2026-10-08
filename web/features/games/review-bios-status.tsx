"use client";
import Link from "next/link";
import { useCallback, useState } from "react";
import type { Schema } from "@/lib/api/types";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { MissingBiosList } from "./missing-bios-list";

export function ReviewBiosStatus({ detail }: { detail: Schema<"GameDetail"> }) {
  const [revision, setRevision] = useState(0);
  const source = JSON.stringify([detail.game.id, detail.game.version, detail.defaultCoreId, detail.coreIds,
    detail.runtimeConfig, detail.files.map(({ id, sha256 }) => [id, sha256]), revision]);
  return <ReviewBiosCheck key={source} gameId={detail.game.id} version={detail.game.version} onReload={() => setRevision((value) => value + 1)} />;
}

function ReviewBiosCheck({ gameId, version, onReload }: { gameId: string; version: number; onReload: () => void }) {
  const load = useCallback(async () => result(await api.POST("/api/v1/admin/reviews/readiness", {
    body: { gameIds: [gameId] },
  })).items.find((item) => item.id === gameId), [gameId]);
  const check = useResource(load);
  const outcome = check.loading ? { message: "正在检查已保存配置所需的 BIOS…", error: false, missing: [] }
    : biosOutcome(check.data, check.error, version);
  return <section className="review-bios-status" aria-label="BIOS 检查">
    <div className="review-bios-status-head">
      <div><h2>BIOS 检查</h2>
        <p role={outcome.error ? "alert" : "status"}>{outcome.message}</p>
      </div>
      <div className="review-bios-status-actions">
        <Link className="button secondary" href="/admin/bios">管理运行依赖</Link>
        <button className="button secondary" disabled={check.loading} onClick={onReload}>{check.loading ? "正在检查…" : "重新检查"}</button>
      </div>
    </div>
    {outcome.missing.length > 0 ? <MissingBiosList items={outcome.missing} /> : null}
    <p className="review-bios-note">按当前已保存的运行配置检查。补齐文件后可重新检查。</p>
  </section>;
}

function biosOutcome(item: Schema<"ReviewReadiness"> | null | undefined, failure: string, version: number) {
  const error = (message: string) => ({ message: `无法检查 BIOS：${message}`, error: true, missing: [] });
  if (failure) { return error(failure); }
  if (item?.error) { return error(item.error.message || "无法检查所需 BIOS，请重试。"); }
  if (!item || item.biosSatisfied === null) { return error("无法检查所需 BIOS，请重试。"); }
  if (item.version !== version) { return error("条目已变化，请刷新页面后重试。"); }
  if (item.biosSatisfied) {
    return { message: "必需 BIOS 已满足。无必需项或仅缺少可选文件时，不影响快速审批。", error: false, missing: [] };
  }
  return { message: `缺少 ${item.missingBios.length} 项必需 BIOS，快速审批会跳过此条目。`, error: false, missing: item.missingBios };
}
