"use client";
import { useState } from "react";
import { api, result, ApiError } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { FeedbackBanner } from "@/components/ui";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import { useToast } from "@/components/toast-provider";
import { SourcePicker } from "./source-picker";
import styles from "./scan.module.css";
async function catalog() {
  return result(await api.GET("/api/v1/runtime/catalog"));
}
export function BiosScan({ onClose }: { onClose: () => void }) {
  const declarations = useResource(catalog);
  const { notify } = useToast();
  const [path, setPath] = useState("/");
  const [sourcePending, setSourcePending] = useState(false);
  const [platformIds, setPlatforms] = useState<string[]>([]);
  const [coreIds, setCores] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function scan() {
    setBusy(true);
    setError("");
    try {
      result(await api.POST("/api/v1/admin/bios-scans", { body: { path, platformIds, coreIds } }));
      notify({ tone: "good", message: "BIOS 扫描已开始。仅补齐明确匹配的缺失要求，已有安装保持原样。" });
      onClose();
    } catch (failure) {
      const message = failure instanceof Error ? failure.message : "扫描失败。";
      if (failure instanceof ApiError && failure.status === 400) { setError(message); }
      else { notify({ tone: "bad", message }); }
    } finally { setBusy(false); }
  }
  return <ResponsiveSheet open title="BIOS扫描" placement="right" className={styles.sheet} busy={busy}
    description="从服务器目录补齐缺失文件；已安装项保持原样。" onClose={onClose}
    footer={<><button className="button secondary" disabled={busy} onClick={onClose}>取消</button>
      <button className="button" disabled={busy || sourcePending || !path.startsWith("/") || (!platformIds.length && !coreIds.length)} onClick={() => void scan()}>
        {busy ? "正在启动…" : "开始扫描"}
      </button></>}>
    <section className="stack">
      <SourcePicker path={path} onChange={setPath} onPendingChange={setSourcePending} />
      <p className="workspace-note">已安装项跳过，仅补齐明确匹配的缺失要求。未匹配或存在歧义时，请手动上传。</p>
      <Scope title="平台范围" items={declarations.data?.platforms ?? []} selected={platformIds} onChange={setPlatforms} />
      <Scope title="核心范围" items={declarations.data?.cores ?? []} selected={coreIds} onChange={setCores} />
      {declarations.error ? <FeedbackBanner tone="bad">{declarations.error}</FeedbackBanner> : null}
      {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
    </section>
  </ResponsiveSheet>;
}
function Scope({ title, items, selected, onChange }: {
  title: string;
  items: { id: string; name: string }[];
  selected: string[];
  onChange: (value: string[]) => void;
}) {
  return <fieldset className={styles.scope}><legend>{title}</legend><div className="workspace-tags">
    {items.map((item) => <label key={item.id}><input type="checkbox" checked={selected.includes(item.id)}
      onChange={(event) => onChange(event.target.checked ? [...selected, item.id] : selected.filter((id) => id !== item.id))} />{item.name}</label>)}
  </div></fieldset>;
}
