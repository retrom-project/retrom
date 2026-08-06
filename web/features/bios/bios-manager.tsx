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
      setNotice("正在验证 BIOS 内容并创建 installation revision…");
      const response = await fetch(`/api/v1/admin/bios/${requirement.id}/installations`, {
        method: "POST",
        credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${requirement.version}"`, "Idempotency-Key": newUuid() }),
        body: JSON.stringify({ uploadFileId: upload.uploadFileId })
      });
      if (!response.ok) throw new Error(await responseError(response, "BIOS 安装失败"));
      const installed = await response.json() as { installationId: string; status: string };
      setNotice(`已创建 installation ${installed.installationId} · ${installed.status}`);
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
    <section className="panel table-wrap"><table><thead><tr><th>逻辑文件</th><th>核心</th><th>需求</th><th>校验状态</th><th>版本</th><th>操作</th></tr></thead><tbody>{items.map((item) => <tr key={item.id}><td><strong>{item.logicalName}</strong><small>{item.expectedMd5 ? `MD5 ${item.expectedMd5}` : "未声明精确 hash，安装后显示校验结果"}</small></td><td>{item.coreName}<small>{item.coreArtifactId.slice(0, 12)}…</small></td><td>{item.requirementMode}</td><td><StatusBadge tone={tone(item.status)}>{item.status}</StatusBadge></td><td>v{item.version}</td><td><input ref={(element) => { inputs.current[item.id] = element; }} className="sr-only" id={`bios-${item.id}`} type="file" disabled={busy !== null} onChange={(event) => { const file = event.target.files?.[0]; if (file) void install(item, file); }} /><label className="row-action" aria-disabled={busy !== null} htmlFor={`bios-${item.id}`}>{busy === item.id ? "安装中…" : "安装 revision"}</label></td></tr>)}</tbody></table></section>
  </div>;
}
