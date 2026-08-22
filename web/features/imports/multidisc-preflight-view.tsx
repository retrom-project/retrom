"use client";

import { useState } from "react";
import { formatBytes } from "@/lib/backend";
import type { MultiDiscGroupPreview, MultiDiscPreflight as MultiDiscPreflightResult } from "./multidisc-preflight";

const stateLabel = {
  COMPLETE: "目录完整",
  BLOCKED: "缺少光盘",
  REJECTED: "不可处理",
} as const;

function initialExpanded(result: MultiDiscPreflightResult) {
  const firstAttention = result.groups.find((group) => group.state !== "COMPLETE");
  return new Set(firstAttention ? [firstAttention.directory] : []);
}

function GroupDetails({ group }: { group: MultiDiscGroupPreview }) {
  return <div className="multi-disc-preflight-group-body">
    {group.reason ? <div className="feedback bad" role="alert"><strong>{group.reason}</strong><code>{group.reasonCode}</code></div> : null}
    {group.entries.length ? <ol className="multi-disc-preflight-discs">
      {group.entries.map((entry) => <li className={entry.state === "MISSING" ? "is-missing" : ""} key={entry.discIndex}>
        <span><strong>{entry.label}</strong><small title={entry.sourceReference}>{entry.sourceReference}</small></span>
        <code>{entry.canonicalName}</code>
        <span>{entry.sizeBytes === null ? "等待上传" : formatBytes(entry.sizeBytes)}</span>
        <span className={`status ${entry.state === "PRESENT" ? "good" : "warn"}`}><i />{entry.state === "PRESENT" ? "已找到" : "缺失"}</span>
      </li>)}
    </ol> : null}
    <div className="multi-disc-preflight-facts">
      <span>当前大小 <strong>{formatBytes(group.presentTotalBytes)}</strong></span>
      <span>目录 <strong title={group.directory}>{group.directory}</strong></span>
      <span>未引用 <strong>{group.ignoredCount} 个</strong></span>
    </div>
    {group.ignoredCount ? <details className="multi-disc-preflight-ignored"><summary>将忽略的同目录文件（{group.ignoredCount}）</summary><p>{group.ignored.slice(0, 20).join("、")}{group.ignoredCount > 20 ? ` 等 ${group.ignoredCount} 个` : ""}</p></details> : null}
  </div>;
}

function MultiDiscPreflightState({ result, allowIncomplete }: { result: MultiDiscPreflightResult; allowIncomplete: boolean }) {
  const [expanded, setExpanded] = useState(() => initialExpanded(result));

  function toggle(directory: string) {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(directory)) {next.delete(directory);} else {next.add(directory);}
      return next;
    });
  }

  return <section className="multi-disc-preflight" aria-labelledby="multi-disc-preflight-title">
    <header>
      <div><h3 id="multi-disc-preflight-title">多盘目录预检</h3><p>本地结果用于即时反馈；上传后仍由服务器按冻结目录重新校验。</p></div>
      <span className={`status ${result.rejectedGroupCount || result.blockedGroupCount && !allowIncomplete ? "bad" : result.blockedGroupCount ? "warn" : "good"}`}><i />{result.rejectedGroupCount ? "包含不可处理目录" : result.blockedGroupCount ? allowIncomplete ? "可以继续，审核会阻断" : "目录不完整，不能替换" : "目录完整，可以上传"}</span>
    </header>
    <div className="multi-disc-preflight-summary">
      <div><span>发现多盘游戏</span><strong>{result.processableGroupCount}</strong></div>
      <div><span>内容完整</span><strong>{result.completeGroupCount}</strong></div>
      <div><span>待补齐</span><strong>{result.blockedGroupCount}</strong></div>
      <div><span>不可处理目录</span><strong>{result.rejectedGroupCount}</strong></div>
      <div><span>未关联文件</span><strong>{result.ignoredFileCount}</strong></div>
    </div>
    <div className="multi-disc-preflight-groups">
      {result.groups.map((group) => {
        const open = expanded.has(group.directory);
        const title = group.playlist || "多个 M3U";
        return <article key={group.directory}>
          <button type="button" aria-expanded={open} onClick={() => toggle(group.directory)}>
            <span><strong title={group.directory}>{group.directory}</strong><small title={title}>{title}</small></span>
            <span>{group.discCount ? `${group.presentDiscCount} / ${group.discCount} 张` : "未形成盘组"}</span>
            <span className={`status ${group.state === "COMPLETE" ? "good" : group.state === "BLOCKED" ? "warn" : "bad"}`}><i />{stateLabel[group.state]}</span>
            <b aria-hidden="true">{open ? "−" : "+"}</b>
          </button>
          {open ? <GroupDetails group={group} /> : null}
        </article>;
      })}
    </div>
    {result.unassociatedFiles.length ? <details className="multi-disc-preflight-unassociated"><summary>未关联目录文件，将忽略（{result.unassociatedFiles.length}）</summary><p>{result.unassociatedFiles.slice(0, 20).join("、")}{result.unassociatedFiles.length > 20 ? ` 等 ${result.unassociatedFiles.length} 个` : ""}</p></details> : null}
    {result.blockedGroupCount ? <div className={`feedback ${allowIncomplete ? "warn" : "bad"}`} role="status"><strong>{allowIncomplete ? "可以继续导入。" : "不能替换当前内容。"}</strong><p>{allowIncomplete ? "审核页面会保留缺失光盘的位置；补齐全部缺失光盘前不能发布。" : "内容替换必须选择同一目录中完整的一组 M3U 与全部引用 CHD。"}</p></div> : null}
  </section>;
}

export function MultiDiscPreflight({ result, allowIncomplete = true }: { result: MultiDiscPreflightResult; allowIncomplete?: boolean }) {
  const resetKey = result.groups.map((group) => `${group.directory}:${group.state}:${group.presentDiscCount}`).join("|");
  return <MultiDiscPreflightState key={resetKey} result={result} allowIncomplete={allowIncomplete} />;
}
