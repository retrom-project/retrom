"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast } from "@/components/flash-toast";
import { api, writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import { responseError } from "@/lib/upload";
import type { SourceImportSummary } from "./source-import-model";

export function SourcePlanDelete({ summary, disabled }: { summary: SourceImportSummary; disabled: boolean }) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  if (summary.importJobId || !["AWAITING_MAPPING", "EXPIRED"].includes(summary.state)) {return null;}

  async function deletePlan() {
    setBusy(true);
    setError("");
    try {
      const { response } = await api.DELETE("/api/v1/admin/source-imports/{sourceImportId}", {
        params: {
          path: { sourceImportId: summary.id },
          header: { ...writeHeaders(), "If-Match": `"v${summary.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" },
        },
      });
      if (!response.ok) {throw new Error(await responseError(response, "删除计划失败"));}
      setOpen(false);
      router.push("/admin/imports/server");
      router.refresh();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "删除计划失败");
    } finally {
      setBusy(false);
    }
  }

  return <>
    <button type="button" className="button danger" disabled={disabled || busy} onClick={() => setOpen(true)}>删除计划</button>
    <ConfirmDialog open={open} title="删除这份未执行计划？" description="只删除等待映射或已过期且没有执行结果的计划。来源目录不会改变。" confirmLabel="删除计划" tone="danger" busy={busy} onCancel={() => setOpen(false)} onConfirm={() => void deletePlan()} />
    <Toast toast={error ? { message: error, tone: "bad" } : null} onDismiss={() => setError("")} />
  </>;
}
