"use client";

import { useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { StatusBadge } from "@/components/ui";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import { responseError, uploadOne } from "@/lib/upload";

export type BIOSRequirement = {
  id: string;
  coreId: string;
  coreName: string;
  coreArtifactId: string;
  logicalName: string;
  requirementMode: string;
  enabled: boolean;
  version: number;
  status: string;
  expectedMd5?: string | null;
};

function tone(status: string): "good" | "warn" | "bad" {
  if (status === "MATCHED") return "good";
  if (status === "MISSING") return "bad";
  return "warn";
}

const statusLabels: Record<string, string> = { MATCHED: "已安装并匹配", MISSING: "缺少文件", MISMATCHED: "文件不匹配", HASH_WARNING: "校验值不一致", MISSING_ENTRY: "归档内缺少文件", INVALID: "文件无效", UNVERIFIED: "等待验证" };
const requirementLabels: Record<string, string> = { REQUIRED: "必需", OPTIONAL: "可选", CONDITIONAL: "按需" };

export function BIOSManager({ items }: { items: BIOSRequirement[] }) {
  const router = useRouter();
  const inputs = useRef<Record<string, HTMLInputElement | null>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  async function install(requirement: BIOSRequirement, file: File) {
    setBusy(requirement.id); setError(""); setNotice("");
    try {
      const upload = await uploadOne(file, setNotice);
      setNotice("正在验证 BIOS 内容并保存安装记录…");
      const response = await fetch(`/api/v1/admin/bios/${requirement.id}/installations`, {
        method: "POST",
        credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${requirement.version}"`, "Idempotency-Key": newUuid() }),
        body: JSON.stringify({ uploadFileId: upload.uploadFileId })
      });
      if (!response.ok) throw new Error(await responseError(response, "BIOS 安装失败"));
      const installed = await response.json() as { installationId: string; status: string };
      setNotice(`BIOS 已安装：${statusLabels[installed.status] ?? "验证完成"}`);
      router.refresh();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "BIOS 安装失败");
    } finally {
      setBusy(null);
      const input = inputs.current[requirement.id];
      if (input) input.value = "";
    }
  }

  return <div className="stack">
    {notice ? <p role="status" className="status good">{notice}</p> : null}
    {error ? <p role="alert" className="status bad">{error}</p> : null}
    <section className="panel table-wrap"><table><thead><tr><th>BIOS 文件</th><th>适用核心</th><th>状态</th><th>操作</th></tr></thead><tbody>{items.map((item) => <tr key={item.id}><td><strong>{item.logicalName}</strong><small>{requirementLabels[item.requirementMode] ?? item.requirementMode}{item.expectedMd5 ? ` · 期望 MD5 ${item.expectedMd5}` : " · 安装后验证"}</small></td><td>{item.coreName}</td><td><StatusBadge tone={tone(item.status)}>{statusLabels[item.status] ?? item.status}</StatusBadge></td><td><input ref={(element) => { inputs.current[item.id] = element; }} hidden id={`bios-${item.id}`} type="file" disabled={busy !== null} onChange={(event) => { const file = event.target.files?.[0]; if (file) void install(item, file); }} /><button className="button secondary compact" type="button" disabled={busy !== null} onClick={() => inputs.current[item.id]?.click()}>{busy === item.id ? "安装中…" : "选择 BIOS 文件"}</button></td></tr>)}</tbody></table></section>
  </div>;
}
