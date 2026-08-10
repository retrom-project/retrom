"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { formatBytes } from "@/lib/backend";

function asciiFold(value: string) {
  return value.replace(/[A-Z]/g, (character) => character.toLowerCase());
}

export type MultiDiscAttachmentSelection = {
  missing: string[];
  unexpected: string[];
  duplicates: string[];
  selectedBytes: number;
  complete: boolean;
};

export function validateMultiDiscAttachmentSelection(
  files: File[],
  missingReferences: string[],
  presentBytes: number,
  maxTotalBytes: number,
): MultiDiscAttachmentSelection {
  const byFoldedName = new Map<string, File[]>();
  for (const file of files) {
    const folded = asciiFold(file.name);
    byFoldedName.set(folded, [...(byFoldedName.get(folded) ?? []), file]);
  }
  const duplicates = [...byFoldedName.values()].filter((matches) => matches.length > 1).flatMap((matches) => matches.map((file) => file.name));
  const used = new Set<File>();
  const missing = missingReferences.filter((reference) => {
    const exact = files.find((file) => file.name === reference && !used.has(file));
    if (exact) { used.add(exact); return false; }
    const folded = byFoldedName.get(asciiFold(reference))?.filter((file) => !used.has(file)) ?? [];
    if (folded.length === 1) { used.add(folded[0]); return false; }
    return true;
  });
  const unexpected = files.filter((file) => !used.has(file)).map((file) => file.name);
  const selectedBytes = files.reduce((total, file) => total + file.size, 0);
  return {
    missing,
    unexpected,
    duplicates,
    selectedBytes,
    complete: files.length > 0 && missing.length === 0 && unexpected.length === 0 && duplicates.length === 0 && presentBytes + selectedBytes <= maxTotalBytes,
  };
}

export function MultiDiscAttachmentDrawer({
  open,
  missingReferences,
  presentBytes,
  maxTotalBytes,
  busy,
  progress,
  onClose,
  onSubmit,
}: {
  open: boolean;
  missingReferences: string[];
  presentBytes: number;
  maxTotalBytes: number;
  busy: boolean;
  progress: string;
  onClose: () => void;
  onSubmit: (files: File[], onQueued: () => void) => Promise<void>;
}) {
  const [files, setFiles] = useState<File[]>([]);
  const drawerRef = useRef<HTMLElement>(null);
  const titleRef = useRef<HTMLHeadingElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const busyRef = useRef(busy);
  const onCloseRef = useRef(onClose);
  useEffect(() => { busyRef.current = busy; }, [busy]);
  useEffect(() => { onCloseRef.current = onClose; }, [onClose]);
  const selection = useMemo(
    () => validateMultiDiscAttachmentSelection(files, missingReferences, presentBytes, maxTotalBytes),
    [files, maxTotalBytes, missingReferences, presentBytes],
  );

  const close = () => {
    if (busy) return;
    setFiles([]);
    onClose();
  };

  useEffect(() => {
    if (!open) return;
    previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    titleRef.current?.focus();
    const keydown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busyRef.current) { event.preventDefault(); setFiles([]); onCloseRef.current(); return; }
      if (event.key !== "Tab") return;
      const focusable = [...(drawerRef.current?.querySelectorAll<HTMLElement>("button:not(:disabled),input:not(:disabled)") ?? [])];
      if (!focusable.length) return;
      const first = focusable[0];
      const last = focusable.at(-1)!;
      if (event.shiftKey && (document.activeElement === first || document.activeElement === titleRef.current)) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    };
    document.addEventListener("keydown", keydown);
    return () => {
      document.removeEventListener("keydown", keydown);
      previousFocusRef.current?.focus();
    };
  }, [open]);

  if (!open) return null;
  const overLimit = presentBytes + selection.selectedBytes > maxTotalBytes;
  return <>
    <button className="review-multidisc-drawer-backdrop" type="button" tabIndex={-1} aria-label="关闭上传全部缺失光盘" onClick={close} />
    <aside ref={drawerRef} className="review-multidisc-drawer" role="dialog" aria-modal="true" aria-labelledby="multi-disc-attachment-title">
      <header><div><span className="eyebrow">补充多盘内容</span><h2 id="multi-disc-attachment-title" ref={titleRef} tabIndex={-1}>上传全部缺失光盘</h2><p>必须一次选择当前列表中的全部 CHD，不能多选或漏选。</p></div><button className="review-multidisc-drawer-close" type="button" aria-label="关闭" disabled={busy} onClick={close}>×</button></header>
      <div className="review-multidisc-drawer-body">
        <section><h3>当前缺失</h3><ol>{missingReferences.map((reference) => {
          const selected = files.some((file) => file.name === reference || asciiFold(file.name) === asciiFold(reference));
          return <li key={reference}><strong>{reference}</strong><span className={`status ${selected ? "good" : "warn"}`}><i />{selected ? "已选择" : "缺少"}</span></li>;
        })}</ol></section>
        <label className="review-multidisc-drawer-picker">选择当前全部缺失 CHD<input aria-label="选择当前全部缺失 CHD" type="file" accept=".chd" multiple disabled={busy} onChange={(event) => setFiles(Array.from(event.currentTarget.files ?? []))} /></label>
        <div className="review-multidisc-drawer-summary" aria-live="polite"><span>已选 {files.length} 个文件 · {formatBytes(selection.selectedBytes)}</span><span>补齐后 {formatBytes(presentBytes + selection.selectedBytes)} / 上限 {formatBytes(maxTotalBytes)}</span></div>
        {selection.missing.length ? <p className="field-error" role="alert">仍缺少：{selection.missing.join("、")}</p> : null}
        {selection.unexpected.length ? <p className="field-error" role="alert">不需要：{selection.unexpected.join("、")}</p> : null}
        {selection.duplicates.length ? <p className="field-error" role="alert">文件名重复：{selection.duplicates.join("、")}</p> : null}
        {overLimit ? <p className="field-error" role="alert">补齐后的光盘总大小超过 {formatBytes(maxTotalBytes)} 上限。</p> : null}
        {progress ? <p className="review-multidisc-drawer-progress" role="status"><i className="button-spinner" aria-hidden="true" />{progress}</p> : null}
      </div>
      <footer><button className="button secondary" type="button" disabled={busy} onClick={close}>取消</button><button className="button" type="button" disabled={busy || !selection.complete} aria-busy={busy} onClick={() => void onSubmit(files, () => { setFiles([]); onClose(); })}>{busy ? "正在上传并校验…" : "上传并校验"}</button></footer>
    </aside>
  </>;
}
