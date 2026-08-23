"use client";

import Link from "next/link";
import { useEffect, useRef, type KeyboardEvent } from "react";
import { AppIcon } from "@/components/app-icon";
import { Toast } from "@/components/flash-toast";
import { TagChips, TagPicker, type TagReference } from "@/components/tag-picker";
import { StatusBadge } from "@/components/ui";
import { formatBytes } from "@/lib/backend";
import type { ServerImportRoot } from "./server-import-manager";
import {
  emulationStationPhaseLabels,
  type EmulationStationCollection,
  type EmulationStationDirectory,
  type EmulationStationGamelist,
  type EmulationStationImportSummary,
  type EmulationStationPlatformInstance,
} from "./emulationstation-import-model";

export type EmulationStationMappingDraft = {
  action: "" | "IMPORT" | "SKIP";
  platformInstanceId: string;
  tags: TagReference[];
};

export type EmulationStationDrawerProps = {
  roots: ServerImportRoot[];
  rootId: string;
  path: string;
  breadcrumbs: string[];
  directories: EmulationStationDirectory[];
  directoryCursor: string | null;
  directoryLoading: boolean;
  selectedRoot?: ServerImportRoot;
  step: 1 | 2 | 3;
  plan: EmulationStationImportSummary | null;
  gamelists: EmulationStationGamelist[];
  collections: EmulationStationCollection[];
  mappings: Record<string, EmulationStationMappingDraft>;
  instances: EmulationStationPlatformInstance[];
  activeTags: TagReference[];
  batchTags: TagReference[];
  batchStatus: string;
  busy: boolean;
  error: string;
  mapped: number;
  skipped: number;
  taggedCollections: number;
  taggedGames: number;
  mappedTags: TagReference[];
  mappingComplete: boolean;
  onRoot: (id: string) => void;
  onPath: (path: string) => void;
  onMore: () => void;
  onBatchTags: (tags: TagReference[]) => void;
  onApplyBatch: () => void;
  onMapping: (id: string, draft: EmulationStationMappingDraft) => void;
  onClose: () => void;
  onScan: () => void;
  onConfirm: () => void;
  onStart: () => void;
  onDismissError: () => void;
};

function trapFocus(drawer: HTMLElement | null, event: KeyboardEvent<HTMLElement>, busy: boolean, close: () => void) {
  if (event.key === "Escape" && !busy) {
    event.preventDefault();
    close();
    return;
  }
  if (event.key !== "Tab") {
    return;
  }
  const selector = "button:not(:disabled),input:not(:disabled),select:not(:disabled),a[href],[tabindex]:not([tabindex='-1'])";
  const focusable = Array.from(drawer?.querySelectorAll<HTMLElement>(selector) ?? []);
  const first = focusable[0];
  const last = focusable.at(-1);
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first?.focus();
  }
}

function RootOptions({ props }: { props: EmulationStationDrawerProps }) {
  return <fieldset className="server-root-options">
    <legend>服务器位置</legend>
    {props.roots.map((root) => <label key={root.id}>
      <input
        type="radio"
        name="emulationstation-root"
        checked={props.rootId === root.id}
        disabled={props.busy || root.status !== "AVAILABLE"}
        onChange={() => props.onRoot(root.id)}
      />
      <span><strong>{root.label}</strong><small>{root.status === "AVAILABLE" ? "可用" : "不可用"}</small></span>
    </label>)}
  </fieldset>;
}

function DirectoryBrowser({ props }: { props: EmulationStationDrawerProps }) {
  const emptyCopy = props.directoryLoading ? "正在读取子目录…" : "当前目录没有可进入的子目录。可以直接扫描此目录。";
  return <div className="server-directory-browser">
    <nav aria-label="当前目录">
      <button type="button" onClick={() => props.onPath("")} disabled={!props.path || props.busy}>根目录</button>
      {props.breadcrumbs.map((part, index) => <button
        type="button"
        key={`${part}-${index}`}
        disabled={index === props.breadcrumbs.length - 1 || props.busy}
        onClick={() => props.onPath(props.breadcrumbs.slice(0, index + 1).join("/"))}
      >/ {part}</button>)}
    </nav>
    {props.directories.length ? <>
      <ul>{props.directories.map((directory) => <li key={directory.relativePath}>
        <button type="button" disabled={props.busy} onClick={() => props.onPath(directory.relativePath)}>
          <AppIcon name="folder" />
          <span>{directory.name}</span>
        </button>
      </li>)}</ul>
      {props.directoryCursor ? <button
        type="button"
        className="button secondary compact server-directory-more"
        disabled={props.directoryLoading || props.busy}
        onClick={props.onMore}
      >{props.directoryLoading ? "正在读取…" : "加载更多目录"}</button> : null}
    </> : <p role="status">{emptyCopy}</p>}
  </div>;
}

function SelectionStep({ props }: { props: EmulationStationDrawerProps }) {
  return <>
    <RootOptions props={props} />
    <DirectoryBrowser props={props} />
    <div className="server-import-selection-summary">
      <strong>{props.selectedRoot?.label ?? "未选择"} / {props.path || "根目录"}</strong>
      <span>递归读取精确小写 gamelist.xml；扫描不会执行 command、emulator 或 core，也不会复制完整游戏内容。</span>
    </div>
  </>;
}

type CollectionMappingProps = {
  collection: EmulationStationCollection;
  draft: EmulationStationMappingDraft;
  instances: EmulationStationPlatformInstance[];
  tags: TagReference[];
  busy: boolean;
  onChange: (draft: EmulationStationMappingDraft) => void;
};

function CollectionMapping({ collection, draft, instances, tags, busy, onChange }: CollectionMappingProps) {
  const value = draft.action === "SKIP" ? "SKIP" : draft.platformInstanceId ? `IMPORT:${draft.platformInstanceId}` : "";
  const extensions = collection.extensionSummary.map((entry) => `${entry.extension || "无扩展名"} ${entry.count}`).join(" · ");
  function select(next: string) {
    if (next === "SKIP") {
      onChange({ action: "SKIP", platformInstanceId: "", tags: [] });
      return;
    }
    if (next.startsWith("IMPORT:")) {
      onChange({ action: "IMPORT", platformInstanceId: next.slice(7), tags: draft.tags });
      return;
    }
    onChange({ action: "", platformInstanceId: "", tags: [] });
  }
  return <article className={collection.hiddenGameCount || collection.adultGameCount ? "has-source-flags" : ""}>
    <div>
      <h3>{collection.displayName}</h3>
      <p>{collection.gamelistRelativePath}</p>
      <small>
        {collection.gameCount} 个游戏 · {extensions || "未发现扩展名"}
        {collection.extensionOtherCount ? ` · 另 ${collection.extensionOtherCount} 项` : ""}
      </small>
      <small>
        {collection.issueCount} 个问题 · 忽略 {collection.folderEntryCount} 个 folder · hidden {collection.hiddenGameCount} · adult {collection.adultGameCount}
      </small>
    </div>
    <label>
      <span>处理方式</span>
      <select aria-label={`${collection.displayName} 处理方式`} value={value} disabled={busy} onChange={(event) => select(event.target.value)}>
        <option value="">请选择，不会自动映射</option>
        <option value="SKIP">跳过此清单</option>
        {instances.map((instance) => <option value={`IMPORT:${instance.id}`} key={instance.id}>
          导入到 {instance.name} · {instance.defaultCoreName}
        </option>)}
      </select>
    </label>
    {draft.action === "IMPORT" ? <div className="pegasus-collection-tags">
      <TagPicker
        label={`${collection.displayName} 的默认标签`}
        options={tags}
        selected={draft.tags}
        disabled={busy}
        onChange={(selected) => onChange({ ...draft, tags: selected })}
        description="此清单生成的每个待审核游戏都会继承这些标签。"
      />
    </div> : null}
  </article>;
}

function ScanProgress({ plan }: { plan: EmulationStationImportSummary }) {
  return <div className="pegasus-scan-progress" aria-live="polite">
    <span className="button-spinner" />
    <h3>{plan.phase ? emulationStationPhaseLabels[plan.phase] : "扫描准备中"}</h3>
    <p>
      已发现 {plan.counts.gamelists} 份清单、{plan.counts.collections} 个有效 Collection、{plan.counts.games} 个游戏。离开页面不会停止任务。
    </p>
  </div>;
}

function MappingSummary({ props, plan }: { props: EmulationStationDrawerProps; plan: EmulationStationImportSummary }) {
  const invalid = props.gamelists.filter((item) => item.parseState === "INVALID");
  return <>
    <div className="pegasus-scan-summary">
      <div><span>Gamelist</span><strong>{plan.counts.gamelists}</strong></div>
      <div><span>有效 Collection</span><strong>{plan.counts.collections}</strong></div>
      <div><span>Game</span><strong>{plan.counts.games}</strong></div>
      <div><span>无效清单</span><strong>{plan.counts.invalidGamelists}</strong></div>
    </div>
    {invalid.length ? <details className="emulationstation-invalid-gamelists">
      <summary>{invalid.length} 份清单无法解析</summary>
      {invalid.map((item) => <p key={item.relativePath}>
        <code>{item.relativePath}</code>
        <span>{item.errorCode ?? "EMULATIONSTATION_XML_INVALID"}</span>
      </p>)}
    </details> : null}
  </>;
}

function BatchTags({ props }: { props: EmulationStationDrawerProps }) {
  const gameCount = props.collections.reduce((total, collection) => total + collection.gameCount, 0);
  return <section className="pegasus-batch-tags">
    <header>
      <div><h3>批量添加默认标签</h3><p>去重追加到所有未跳过清单；下方仍可逐项增删。</p></div>
      <span>{gameCount} 个游戏</span>
    </header>
    <TagPicker
      label="批次标签"
      options={props.activeTags}
      selected={props.batchTags}
      disabled={props.busy}
      onChange={props.onBatchTags}
      description="标签必须先在标签管理中建立。"
    />
    <div className="pegasus-batch-tag-actions">
      <button
        type="button"
        className="button secondary compact"
        disabled={props.busy || !props.batchTags.length}
        onClick={props.onApplyBatch}
      >应用到所有未跳过清单</button>
      {props.batchStatus ? <p role="status">{props.batchStatus}</p> : null}
    </div>
  </section>;
}

function MappingStep({ props }: { props: EmulationStationDrawerProps }) {
  const plan = props.plan;
  if (plan?.state === "SCANNING") {
    return <ScanProgress plan={plan} />;
  }
  if (plan?.state === "FAILED") {
    return <div className="runtime-inline-empty">
      <h3>扫描未完成</h3>
      <p>{plan.lastErrorCode ?? "EmulationStation 扫描任务失败"}</p>
    </div>;
  }
  if (plan?.state !== "AWAITING_MAPPING") {
    return null;
  }
  if (!props.instances.length) {
    return <div className="runtime-inline-empty">
      <h3>还没有游戏目录</h3>
      <p>请先使用“一键创建推荐目录”建立映射目标，再回来继续这次 EmulationStation 导入。</p>
      <Link className="button" href="/admin/platform-instances">前往游戏目录</Link>
    </div>;
  }
  return <>
    <MappingSummary props={props} plan={plan} />
    <p className="pegasus-mapping-note">每份有效 gamelist.xml 都必须明确选择游戏目录或跳过。Retrom 不根据目录名、扩展名或外部平台配置猜测。</p>
    <BatchTags props={props} />
    <div className="pegasus-collection-list emulationstation-collection-list">
      {props.collections.map((collection) => <CollectionMapping
        key={collection.id}
        collection={collection}
        draft={props.mappings[collection.id] ?? { action: "", platformInstanceId: "", tags: [] }}
        instances={props.instances}
        tags={props.activeTags}
        busy={props.busy}
        onChange={(draft) => props.onMapping(collection.id, draft)}
      />)}
    </div>
  </>;
}

function ReviewStep({ props }: { props: EmulationStationDrawerProps }) {
  const plan = props.plan;
  if (!plan) {
    return null;
  }
  const flagged = props.collections.reduce((total, item) => total + item.hiddenGameCount + item.adultGameCount, 0);
  return <>
    <div className="pegasus-review-table">
      <div><span>来源</span><strong>{plan.root.label} / {plan.sourceRelativePath || "根目录"}</strong></div>
      <div><span>映射</span><strong>{props.mapped} 个处理 · {props.skipped} 个跳过</strong></div>
      <div><span>默认标签覆盖</span><strong>{props.taggedCollections} 个 Collection · {props.taggedGames} 个游戏</strong></div>
      <div><span>可处理 / 阻断</span><strong>{plan.counts.processable} / {plan.counts.blocked} 个游戏</strong></div>
      <div><span>来源提示</span><strong>{flagged} 个 hidden/adult 标记需逐项核对</strong></div>
      <div><span>预计最多读取</span><strong>{formatBytes(plan.counts.estimatedSourceBytes)}</strong></div>
      <div><span>发布方式</span><strong>全部进入待审核，不会自动发布</strong></div>
    </div>
    {props.mappedTags.length ? <div className="pegasus-review-tags">
      <span>本批使用的标签</span>
      <TagChips tags={props.mappedTags} />
    </div> : null}
    <p className="pegasus-mapping-note">
      开始前会重验清单、源文件与目标目录；command、emulator、core 永远不会执行。hidden/adult 项不会进入快速审批，但仍可逐项审核。
    </p>
  </>;
}

function DrawerSteps({ step }: { step: 1 | 2 | 3 }) {
  const className = (expected: number) => step === expected ? "is-active" : step > expected ? "is-complete" : "";
  return <ol className="pegasus-stepper" aria-label="导入步骤">
    <li className={className(1)}><span>1</span>选择目录</li>
    <li className={className(2)}><span>2</span>检查与映射</li>
    <li className={className(3)}><span>3</span>确认审核计划</li>
  </ol>;
}

function DrawerBody({ props }: { props: EmulationStationDrawerProps }) {
  if (props.step === 1) {
    return <SelectionStep props={props} />;
  }
  if (props.step === 2) {
    return <MappingStep props={props} />;
  }
  return <ReviewStep props={props} />;
}

function DrawerPrimaryAction({ props }: { props: EmulationStationDrawerProps }) {
  if (props.step === 1) {
    return <button
      type="button"
      className="button"
      disabled={props.busy || !props.rootId || props.selectedRoot?.status !== "AVAILABLE"}
      onClick={props.onScan}
    >{props.busy ? "正在创建…" : "扫描此目录"}</button>;
  }
  if (props.step === 2 && props.plan?.state === "AWAITING_MAPPING") {
    return <button type="button" className="button" disabled={props.busy || !props.mappingComplete} onClick={props.onConfirm}>
      {props.busy ? "正在保存…" : "确认映射"}
    </button>;
  }
  if (props.step === 3) {
    return <button type="button" className="button" disabled={props.busy} onClick={props.onStart}>
      {props.busy ? "正在启动…" : "开始准备审核事项"}
    </button>;
  }
  return null;
}

export function EmulationStationImportDrawerView(props: EmulationStationDrawerProps) {
  const drawer = useRef<HTMLElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const root = document.documentElement;
    const body = document.body;
    const rootOverflow = root.style.overflow;
    const bodyOverflow = body.style.overflow;
    root.style.overflow = "hidden";
    body.style.overflow = "hidden";
    closeButton.current?.focus({ preventScroll: true });
    return () => {
      root.style.overflow = rootOverflow;
      body.style.overflow = bodyOverflow;
      previous?.focus({ preventScroll: true });
    };
  }, []);
  return <>
    <button
      type="button"
      className="runtime-drawer-backdrop"
      aria-label="关闭 EmulationStation 导入"
      disabled={props.busy}
      onClick={props.onClose}
    />
    <aside
      ref={drawer}
      className="runtime-drawer server-import-drawer pegasus-import-drawer emulationstation-import-drawer"
      role="dialog"
      aria-modal="true"
      aria-labelledby="emulationstation-import-title"
      onKeyDown={(event) => trapFocus(drawer.current, event, props.busy, props.onClose)}
    >
      <header>
        <div>
          <StatusBadge tone="info">EmulationStation</StatusBadge>
          <h2 id="emulationstation-import-title">从 gamelist.xml 准备审核事项</h2>
          <p>每份有效清单形成一个 Collection；扫描只读取受限元数据和文件事实。</p>
        </div>
        <button ref={closeButton} type="button" className="runtime-drawer-close" aria-label="关闭" disabled={props.busy} onClick={props.onClose}>
          <AppIcon name="x" />
        </button>
      </header>
      <DrawerSteps step={props.step} />
      <div className="runtime-drawer-body" tabIndex={0} aria-label="EmulationStation 导入步骤内容">
        <DrawerBody props={props} />
      </div>
      <footer>
        <button type="button" className="button secondary" disabled={props.busy} onClick={props.onClose}>关闭</button>
        <DrawerPrimaryAction props={props} />
      </footer>
    </aside>
    <Toast toast={props.error ? { message: props.error, tone: "bad" } : null} onDismiss={props.onDismissError} />
  </>;
}
