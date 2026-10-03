"use client";

import { useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { api, writeHeaders } from "@/lib/api/client";
import type { components } from "@/lib/api/generated/schema";
import { newUuid } from "@/lib/crypto";
import { responseError } from "@/lib/upload";

type Disposition = components["schemas"]["ImportBatchDiscard"];
type Props = { disposition: Disposition; onChange: (value: Disposition) => void };

export function ImportBatchDiscard({ disposition, onChange }: Props) {
  const { kind, importId, state, errorCode: failureCode } = disposition;
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function discard() {
    setBusy(true);
    setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/import-batches/{kind}/{importId}/discard", {
        params: { path: { kind, importId }, header: { ...writeHeaders(), "Idempotency-Key": newUuid() } },
        body: {},
      });
      if (!data) { throw new Error(await responseError(response, "丢弃请求未成功，请重试")); }
      onChange(data);
      setOpen(false);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "丢弃请求未成功，请重试");
    } finally {
      setBusy(false);
    }
  }

  return <div className="import-batch-discard">
    <button
      type="button" className="button secondary" disabled={busy || ["REQUESTED", "COMPLETED", "UNAVAILABLE"].includes(state)}
      title={state === "UNAVAILABLE" || state === "COMPLETED" ? "本批次没有待丢弃内容" : undefined}
      onClick={() => setOpen(true)}
    >{state === "REQUESTED" ? "正在丢弃…" : state === "FAILED" ? "重试丢弃" : "丢弃"}</button>
    {state === "COMPLETED" ? <span className="sr-only" role="status">未发布内容已丢弃</span> : null}
    {state === "FAILED" ? <small role="alert">{failureCode === "IMPORT_BATCH_DISCARD_RELEASE_FAILED" ? "源文件清理任务失败，请先在任务中心重试清理，再继续丢弃。" : failureCode === "IMPORT_BATCH_DISCARD_OWNER_AMBIGUOUS" ? "旧任务的数据归属不唯一，已保留相关文件；请根据错误码检查任务关联。" : "部分清理未完成，可重试继续处理。"}</small> : null}
    {error ? <small role="alert">{error}</small> : null}
    <ConfirmDialog open={open} title="丢弃本批次未发布内容？" tone="danger"
      description="本批次所有待审核和导入失败的内容都会丢弃，无法继续审核或重试导入。已发布游戏和服务器原始文件保留；无引用文件按现有 GC 保留期回收。"
      confirmLabel="确认丢弃" busy={busy} onConfirm={() => void discard()} onCancel={() => setOpen(false)} />
  </div>;
}
