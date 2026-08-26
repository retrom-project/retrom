"use client";

import { useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast } from "@/components/flash-toast";
import { StatusBadge } from "@/components/ui";
import type { components } from "@/lib/api/generated/schema";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import { responseError, uploadFiles, waitForJob } from "@/lib/upload";

export type RuntimeAssetPackList = components["schemas"]["RuntimeAssetPackList"];
export type CoreArtifactList = components["schemas"]["CoreArtifactList"];
type PackDefinition = components["schemas"]["RuntimeAssetPackDefinition"];
type PackInstallation = components["schemas"]["RuntimeAssetPackInstallation"];
type PackKind = components["schemas"]["RuntimeAssetPackKind"];
type Generation = components["schemas"]["RpgGeneration"];

const generations: Array<{ id: Generation; label: string }> = [
  { id: "RPG2000", label: "2000" },
  { id: "RPG2003", label: "2003" },
  { id: "RPGXP", label: "XP" },
  { id: "RPGVX", label: "VX" },
  { id: "RPGVXACE", label: "VX Ace" },
];

const rpgCoreOrder = [
  "rpgmaker_2000", "rpgmaker_2003", "rpgmaker_xp", "rpgmaker_vx",
  "rpgmaker_vx_ace", "rpgmaker_mv", "rpgmaker_mz",
];

const stateLabels: Record<string, string> = {
  VALIDATING: "验证中", READY: "结构已验证", FAILED: "验证失败",
  DELETE_PENDING: "删除中", DELETED: "已删除",
};

function stateTone(status: string): "good" | "warn" | "bad" | "info" {
  if (status === "READY") {return "good";}
  if (status === "FAILED") {return "bad";}
  return status === "VALIDATING" ? "info" : "warn";
}

function bytes(value: number) {
  if (value < 1024) {return `${value} B`;}
  if (value < 1024 * 1024) {return `${(value / 1024).toFixed(1)} KiB`;}
  return `${(value / 1024 / 1024).toFixed(1)} MiB`;
}

function RuntimePackInstallationRow({ installation, busy, onDelete }: {
  installation: PackInstallation;
  busy: boolean;
  onDelete: (installation: PackInstallation) => void;
}) {
  const references = installation.references.variantRevisionCount + installation.references.checkpointCount;
  const deletable = references === 0 && (installation.status === "READY" || installation.status === "FAILED");
  return <article className="runtime-pack-installation">
    <div>
      <StatusBadge tone={stateTone(installation.status)}>{stateLabels[installation.status] ?? installation.status}</StatusBadge>
      <strong>v{installation.version} · {installation.filesDigest.slice(0, 8)} · {installation.fileCount.toLocaleString("zh-CN")} 个文件 · {bytes(installation.totalBytes)}</strong>
      <small>安装于 {new Date(installation.createdAtMs).toLocaleString("zh-CN")}{installation.sourceNote ? ` · ${installation.sourceNote}` : ""}</small>
    </div>
    <div className="runtime-pack-reference">
      <strong>{references}</strong><small>引用</small>
      <span>{installation.references.variantRevisionCount} 个游戏版本 · {installation.references.checkpointCount} 个存档</span>
    </div>
    <button type="button" className="runtime-delete-button" disabled={busy || !deletable} title={references ? "仍被游戏版本或存档引用，不能删除" : undefined} onClick={() => onDelete(installation)}>删除</button>
    {installation.diagnostics.map((diagnostic) => <p className="runtime-pack-diagnostic" key={`${diagnostic.code}-${diagnostic.message}`}>{diagnostic.code} · {diagnostic.message}</p>)}
  </article>;
}

function RuntimePackDefinitionCard({ definition, installations, busy, onInstall, onDelete }: {
  definition: PackDefinition;
  installations: PackInstallation[];
  busy: boolean;
  onInstall: (definition: PackDefinition) => void;
  onDelete: (installation: PackInstallation) => void;
}) {
  return <article className="runtime-pack-definition">
    <header><div><span>{definition.origin === "BUILTIN" ? "标准定义" : "自定义定义"}</span><h3>{definition.displayName}</h3><p>{definition.declaredName} · {definition.requiredLayoutVersion}</p></div>
      <button className="button secondary compact" type="button" disabled={busy || !definition.enabled} onClick={() => onInstall(definition)}>安装新副本</button>
    </header>
    {installations.length
      ? <div className="runtime-pack-installations">{installations.map((installation) => <RuntimePackInstallationRow key={installation.installationId} installation={installation} busy={busy} onDelete={onDelete} />)}</div>
      : <p className="runtime-pack-empty">尚未安装。游戏需要该运行包时会在审核页阻止发布。</p>}
  </article>;
}

function RuntimeCoreDiagnostics({ catalog }: { catalog: CoreArtifactList }) {
  const artifacts = new Map(catalog.items.filter((item) => item.selectedForNewBindings).map((item) => [item.coreId, item]));
  return <section className="runtime-core-diagnostics" aria-labelledby="runtime-core-diagnostics-title">
    <header><div><span>管理员诊断</span><h2 id="runtime-core-diagnostics-title">版本核心 route / artifact</h2></div><p>内部路线只用于管理审计，不会作为用户核心选项。</p></header>
    <div className="runtime-core-diagnostic-grid">{rpgCoreOrder.map((coreId) => {
      const artifact = artifacts.get(coreId);
      return <article key={coreId}><div><strong>{artifact?.coreName ?? coreId}</strong><small>{coreId}</small></div>
        {artifact ? <><code>{artifact.routeKey}</code><small title={artifact.id}>artifact {artifact.id} · {artifact.runtimeAdapterKind} / {artifact.adapterId}</small><StatusBadge tone={artifact.availableForLaunch ? "good" : "bad"}>{artifact.availableForLaunch ? "可启动" : "构件不可用"}</StatusBadge></> : <StatusBadge tone="bad">未登记</StatusBadge>}
      </article>;
    })}</div>
  </section>;
}

type InstallDraft = {
  kind: PackKind;
  generation: "RPG2000" | "RPG2003" | "RPGXP" | "RPGVX" | "RPGVXACE";
  declaredName: string;
  sourceNote: string;
  files: File[];
  sourceType: "ARCHIVE" | "DIRECTORY";
};

function installDraft(definition?: PackDefinition): InstallDraft {
  const generation = definition && definition.generation !== "RPGMV" && definition.generation !== "RPGMZ"
    ? definition.generation
    : "RPG2000";
  return {
    kind: definition?.kind ?? "RPG2000_RTP",
    generation,
    declaredName: definition?.origin === "CUSTOM" ? definition.declaredName : "",
    sourceNote: "", files: [], sourceType: "ARCHIVE",
  };
}

function RuntimePackInstallDrawer({ definitions, draft, busy, progress, onChange, onClose, onSubmit }: {
  definitions: PackDefinition[];
  draft: InstallDraft | null;
  busy: boolean;
  progress: string;
  onChange: (next: InstallDraft) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const dialog = useRef<HTMLElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const directoryInput = useRef<HTMLInputElement>(null);
  useEffect(() => {directoryInput.current?.setAttribute("webkitdirectory", "");}, []);
  useEffect(() => {if (draft) {closeButton.current?.focus();}}, [draft]);
  if (!draft) {return null;}
  const customGeneration = draft.generation === "RPGXP" || draft.generation === "RPGVX" || draft.generation === "RPGVXACE";
  const selectedDefinition = definitions.find((definition) => definition.kind === draft.kind && definition.generation === draft.generation);
  const trapFocus = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key === "Escape" && !busy) {onClose(); return;}
    if (event.key !== "Tab") {return;}
    const focusable = [...(dialog.current?.querySelectorAll<HTMLElement>("button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled)") ?? [])];
    if (!focusable.length) {return;}
    const first = focusable[0]; const last = focusable.at(-1)!;
    if (event.shiftKey && document.activeElement === first) {event.preventDefault(); last.focus();}
    if (!event.shiftKey && document.activeElement === last) {event.preventDefault(); first.focus();}
  };
  const setFiles = (files: FileList | null) => onChange({ ...draft, files: files ? [...files] : [] });
  const selectGeneration = (generation: InstallDraft["generation"]) => {
    const standard = definitions.find((definition) => definition.generation === generation && definition.origin === "BUILTIN");
    onChange({ ...draft, generation, kind: standard?.kind ?? "RGSS_CUSTOM_RTP", declaredName: "", files: [] });
  };
  const selectedBytes = draft.files.reduce((total, file) => total + file.size, 0);
  const selectedPaths = draft.files.slice(0, 4).map((file) => file.webkitRelativePath || file.name);
  return <>
    <button type="button" className="runtime-drawer-backdrop" aria-label="关闭安装运行包" disabled={busy} onClick={onClose} />
    <aside ref={dialog} className="runtime-drawer runtime-pack-drawer" role="dialog" aria-modal="true" aria-labelledby="runtime-pack-drawer-title" onKeyDown={trapFocus}>
      <form onSubmit={onSubmit}>
        <header><div><StatusBadge tone="info">RPG Maker</StatusBadge><h2 id="runtime-pack-drawer-title">安装 RPG Maker 运行包</h2><p>上传会保留原始内容，并生成受校验的不可变安装副本。</p></div><button ref={closeButton} className="runtime-drawer-close" type="button" aria-label="关闭" disabled={busy} onClick={onClose}><AppIcon name="x" /></button></header>
        <div className="runtime-drawer-body">
          <label>世代<select className="select" value={draft.generation} disabled={busy} onChange={(event) => selectGeneration(event.target.value as InstallDraft["generation"])}>
            {generations.map((generation) => <option value={generation.id} key={generation.id}>{generation.label}</option>)}
          </select></label>
          <label>运行包类型<select className="select" value={draft.kind} disabled={busy} onChange={(event) => onChange({ ...draft, kind: event.target.value as PackKind, declaredName: "", files: [] })}>
            {definitions.filter((definition) => definition.origin === "BUILTIN" && definition.generation === draft.generation).map((definition) => <option value={definition.kind} key={definition.definitionId}>{definition.displayName}</option>)}
            {customGeneration ? <option value="RGSS_CUSTOM_RTP">自定义 RGSS RTP</option> : null}
          </select></label>
          {draft.kind === "RGSS_CUSTOM_RTP"
            ? <label>声明名称<input value={draft.declaredName} maxLength={512} required disabled={busy} onChange={(event) => onChange({ ...draft, declaredName: event.target.value })} /><small>NFKC 输入预览：{draft.declaredName.trim().normalize("NFKC") || "—"}；最终匹配键由服务端完整大小写折叠生成。</small></label>
            : <label>声明名称<input className="runtime-file-name" value={selectedDefinition?.declaredName ?? ""} readOnly /></label>}
          <label>来源说明（可选）<textarea value={draft.sourceNote} maxLength={500} disabled={busy} onChange={(event) => onChange({ ...draft, sourceNote: event.target.value })} /></label>
          <fieldset className="runtime-pack-source"><legend>上传内容</legend><div>
            <label><input type="radio" name="pack-source" checked={draft.sourceType === "ARCHIVE"} disabled={busy} onChange={() => onChange({ ...draft, sourceType: "ARCHIVE", files: [] })} /> ZIP / 7z</label>
            <label><input type="radio" name="pack-source" checked={draft.sourceType === "DIRECTORY"} disabled={busy} onChange={() => onChange({ ...draft, sourceType: "DIRECTORY", files: [] })} /> 整个目录</label>
          </div>
          {draft.sourceType === "ARCHIVE"
            ? <input type="file" accept=".zip,.7z" required disabled={busy} onChange={(event) => setFiles(event.target.files)} />
            : <input ref={directoryInput} type="file" multiple required disabled={busy} onChange={(event) => setFiles(event.target.files)} />}
          <small>{draft.files.length ? `已选择 ${draft.files.length} 个文件 · ${bytes(selectedBytes)}` : "尚未选择内容"}</small>
          {selectedPaths.length ? <ol className="runtime-pack-file-preview">{selectedPaths.map((path) => <li key={path}>{path}</li>)}</ol> : null}</fieldset>
          <p className="runtime-pack-legal">只上传你有权使用的 RPG Maker RTP 或游戏运行依赖。Retrom 不提供、下载或重新分发厂商资源；安装记录会保留你填写的来源说明。</p>
          {progress ? <p className="runtime-drawer-progress"><span className="button-spinner" />{progress}</p> : null}
        </div>
        <footer><button className="button secondary" type="button" disabled={busy} onClick={onClose}>取消</button><button className="button" disabled={busy || draft.files.length === 0}>{busy ? "正在验证…" : "上传并验证"}</button></footer>
      </form>
    </aside>
  </>;
}

export function RuntimeAssetPackManager({ initialList, initialCoreArtifacts }: {
  initialList: RuntimeAssetPackList;
  initialCoreArtifacts: CoreArtifactList;
}) {
  const trigger = useRef<HTMLButtonElement | null>(null);
  const [catalog, setCatalog] = useState(initialList);
  const [draft, setDraft] = useState<InstallDraft | null>(null);
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [deleting, setDeleting] = useState<PackInstallation | null>(null);
  const byDefinition = useMemo(() => {
    const result = new Map<string, PackInstallation[]>();
    for (const installation of catalog.installations) {
      const current = result.get(installation.definitionId) ?? [];
      current.push(installation);
      result.set(installation.definitionId, current);
    }
    return result;
  }, [catalog.installations]);
  const reload = async () => {
    const response = await fetch("/api/v1/admin/runtime-asset-packs", { cache: "no-store" });
    if (!response.ok) {throw new Error(await responseError(response, "运行包列表读取失败"));}
    setCatalog(await response.json() as RuntimeAssetPackList);
  };
  const monitorInstallation = async (jobID: string) => {
    try {
      await reload();
      await waitForJob(jobID);
      await reload();
      setNotice("运行包安装并验证完成");
    } catch (caught) {
      await reload().catch(() => undefined);
      setError(caught instanceof Error ? caught.message : "运行包验证失败");
    }
  };
  const open = (definition?: PackDefinition, target?: HTMLButtonElement) => {
    trigger.current = target ?? document.activeElement as HTMLButtonElement;
    setDraft(installDraft(definition)); setProgress(""); setError("");
  };
  const close = () => {if (!busy) {setDraft(null); trigger.current?.focus();}};
  const submit = async (event: FormEvent) => {
    event.preventDefault(); if (!draft || draft.files.length === 0) {return;}
    setBusy(true); setError("");
    try {
      const uploaded = await uploadFiles(draft.files, setProgress, "RUNTIME_ASSET_PACK");
      setProgress("正在建立不可变安装并验证布局…");
      const body = draft.kind === "RGSS_CUSTOM_RTP"
        ? { uploadId: uploaded.uploadId, kind: draft.kind, generation: draft.generation, declaredName: draft.declaredName.trim(), sourceNote: draft.sourceNote }
        : { uploadId: uploaded.uploadId, kind: draft.kind, sourceNote: draft.sourceNote };
      const response = await fetch("/api/v1/admin/runtime-asset-packs/installations", { method: "POST", headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }), body: JSON.stringify(body) });
      if (!response.ok) {throw new Error(await responseError(response, "运行包安装失败"));}
      const accepted = await response.json() as { jobId: string };
      setDraft(null); setNotice("运行包已上传，正在后台验证");
      void monitorInstallation(accepted.jobId);
    } catch (caught) {
      await reload().catch(() => undefined);
      setError(caught instanceof Error ? caught.message : "运行包安装失败");
    }
    finally {setBusy(false); setProgress("");}
  };
  const remove = async () => {
    if (!deleting) {return;} setBusy(true); setError("");
    try {
      const response = await fetch(`/api/v1/admin/runtime-asset-packs/installations/${deleting.installationId}`, { method: "DELETE", headers: await writeHeaders({ "If-Match": `"v${deleting.version}"`, "Idempotency-Key": newUuid() }) });
      if (!response.ok) {throw new Error(await responseError(response, "运行包删除失败"));}
      setDeleting(null); await reload(); setNotice("运行包已删除；无引用 payload 已进入清理流程");
    } catch (caught) {setError(caught instanceof Error ? caught.message : "运行包删除失败");}
    finally {setBusy(false);}
  };
  const deletingDefinition = deleting
    ? catalog.definitions.find((definition) => definition.definitionId === deleting.definitionId)
    : null;
  return <section className="runtime-pack-manager">
    <div className="runtime-pack-intro"><div><h2>RPG Maker 运行包</h2><p>仅上传你有权使用的运行包。Retrom 不会自动下载或随镜像分发 RTP；审核会冻结具体安装。</p></div><button className="button" type="button" disabled={busy} onClick={(event) => open(undefined, event.currentTarget)}><AppIcon name="plus" />安装运行包</button></div>
    <RuntimeCoreDiagnostics catalog={initialCoreArtifacts} />
    {generations.map((generation) => <section className="runtime-pack-generation" key={generation.id}><header><div><span>RPG Maker</span><h2>{generation.label}</h2></div><p>{generation.id === "RPG2000" || generation.id === "RPG2003" ? "EasyRPG RTP 布局" : "mkxp-z RGSS 运行依赖"}</p></header><div className="runtime-pack-definition-grid">{catalog.definitions.filter((definition) => definition.generation === generation.id).map((definition) => <RuntimePackDefinitionCard key={definition.definitionId} definition={definition} installations={byDefinition.get(definition.definitionId) ?? []} busy={busy} onInstall={(selected) => open(selected)} onDelete={setDeleting} />)}</div></section>)}
    <section className="runtime-pack-native"><div><span>RPG Maker</span><h2>MV / MZ</h2><p>Web 项目固定自包含，不接受 RTP 安装或运行包绑定。</p></div><StatusBadge tone="good">无需运行包</StatusBadge></section>
    <RuntimePackInstallDrawer definitions={catalog.definitions} draft={draft} busy={busy} progress={progress} onChange={setDraft} onClose={close} onSubmit={(event) => void submit(event)} />
    <ConfirmDialog open={deleting !== null} title="删除这个运行包安装？" description={deleting ? `${deletingDefinition?.displayName ?? deleting.definitionId} · v${deleting.version} · ${deleting.fileCount.toLocaleString("zh-CN")} 个文件 / ${bytes(deleting.totalBytes)}。删除后 payload 会进入保留期清理。` : ""} confirmLabel="删除安装" tone="danger" busy={busy} onConfirm={() => void remove()} onCancel={() => !busy && setDeleting(null)} />
    <Toast toast={notice ? { message: notice, tone: "good" } : null} onDismiss={() => setNotice("")} />
    <Toast toast={error ? { message: error, tone: "bad" } : null} onDismiss={() => setError("")} />
  </section>;
}
