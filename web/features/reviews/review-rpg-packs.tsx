"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import type { components } from "@/lib/api/generated/schema";
import { responseError } from "@/lib/upload";
import type { RPGMakerReview } from "./review-actions-model";

type PackList = components["schemas"]["RuntimeAssetPackList"];

export function RPGPackControls({ value, disabled, onChange }: {
  value: RPGMakerReview;
  disabled: boolean;
  onChange: (next: RPGMakerReview) => void;
}) {
  const [catalog, setCatalog] = useState<PackList | null>(null);
  const [error, setError] = useState("");
  const supportsOverride = value.generation === "RPG2000" || value.generation === "RPG2003";
  const needsCatalog = value.runtimePackRequirements.length > 0;
  useEffect(() => {
    if (!needsCatalog) {return;}
    const controller = new AbortController();
    void fetch("/api/v1/admin/runtime-asset-packs", { cache: "no-store", signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) {throw new Error(await responseError(response, "无法读取可用运行包"));}
        setCatalog(await response.json() as PackList);
      })
      .catch((caught: unknown) => {
        if (caught instanceof DOMException && caught.name === "AbortError") {return;}
        setError(caught instanceof Error ? caught.message : "无法读取可用运行包");
      });
    return () => controller.abort();
  }, [needsCatalog]);
  const candidatesBySlot = useMemo(() => {
    const result = new Map<number, PackList["installations"]>();
    if (!catalog) {return result;}
    for (const requirement of value.runtimePackRequirements) {
      const definitionIDs = new Set(catalog.definitions.filter((definition) =>
        definition.enabled && definition.generation === value.generation &&
        definition.normalizedDeclaredName === requirement.normalizedDeclaredName
      ).map((definition) => definition.definitionId));
      result.set(requirement.slot, catalog.installations.filter((installation) =>
        installation.status === "READY" && definitionIDs.has(installation.definitionId)
      ));
    }
    return result;
  }, [catalog, value.generation, value.runtimePackRequirements]);
  const updateSelection = (slot: number, installationId: string) => {
    const requirement = value.runtimePackRequirements.find((entry) => entry.slot === slot);
    const retained = value.runtimePackSelections.filter((selection) => selection.slot !== slot);
    const next = installationId && requirement
      ? [...retained, { slot, declaredName: requirement.declaredName, installationId }]
      : retained;
    onChange({
      ...value,
      runtimePackSelections: next.sort((left, right) => left.slot - right.slot),
      runtimeValidation: null,
    });
  };
  const setOverride = (override: boolean) => onChange({
    ...value,
    selfContainedOverride: override,
    runtimePackSelections: override ? [] : value.runtimePackSelections,
    runtimeValidation: null,
  });
  if (!supportsOverride && !needsCatalog) {return null;}
  return <section className="review-rpg-pack-controls" aria-labelledby="review-rpg-packs-title">
    <div className="review-rpg-pack-heading"><div><strong id="review-rpg-packs-title">运行包绑定</strong><p>选择会冻结具体安装副本，后续安装不会自动替换。</p></div><Link href="/admin/bios?tab=rpgmaker">管理运行包</Link></div>
    {supportsOverride ? <label className="review-rpg-self-contained"><input type="checkbox" checked={value.selfContained || value.selfContainedOverride} disabled={disabled || value.selfContained} onChange={(event) => setOverride(event.target.checked)} /><span><strong>确认项目自包含 RTP</strong><small>{value.selfContained ? "项目证据已确认自包含，无需外部 RTP。" : "仅 2000/2003 可人工确认；启用后不绑定外部 RTP。"}</small></span></label> : null}
    {!value.selfContainedOverride ? value.runtimePackRequirements.map((requirement) => {
      const candidates = candidatesBySlot.get(requirement.slot) ?? [];
      const selected = value.runtimePackSelections.find((selection) => selection.slot === requirement.slot);
      return <label className="review-rpg-pack-select" key={requirement.slot}><span>Slot {requirement.slot} · {requirement.declaredName}</span><select value={selected?.installationId ?? ""} disabled={disabled || !catalog} onChange={(event) => updateSelection(requirement.slot, event.target.value)}><option value="">{catalog ? candidates.length ? "请选择已验证安装" : "没有可用安装" : "正在读取安装…"}</option>{candidates.map((installation) => <option value={installation.installationId} key={installation.installationId}>{installation.fileCount} 文件 · {new Date(installation.createdAtMs).toLocaleString("zh-CN")} · {installation.installationId.slice(0, 8)}</option>)}</select></label>;
    }) : null}
    {error ? <p className="review-rpg-pack-error" role="alert">{error}</p> : null}
  </section>;
}
