"use client";

import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import type { FavoriteFolder } from "./favorite-api";

function DialogFrame({
  open, title, description, role = "dialog", modal = true, anchor, resolveReturnFocus, dismissButton = false, children, onClose,
}: {
  open: boolean; title: string; description?: string; role?: "dialog" | "alertdialog";
  modal?: boolean; anchor?: HTMLElement | null; resolveReturnFocus?: () => HTMLElement | null;
  dismissButton?: boolean; children: ReactNode; onClose: () => void;
}) {
  const titleId = useId();
  const descriptionId = useId();
  const panel = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!open) {return;}
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const frame = window.requestAnimationFrame(() => {
      if (!modal && anchor && panel.current) {
        const anchorRect = anchor.getBoundingClientRect();
        const width = panel.current.offsetWidth;
        const height = panel.current.offsetHeight;
        const left = Math.max(16, Math.min(anchorRect.right - width, window.innerWidth - width - 16));
        const preferredTop = anchorRect.bottom + 8;
        const top = preferredTop + height <= window.innerHeight - 16
          ? preferredTop
          : Math.max(16, anchorRect.top - height - 8);
        panel.current.style.left = `${left}px`;
        panel.current.style.top = `${top}px`;
      }
      const target = panel.current?.querySelector<HTMLElement>("[data-dialog-autofocus]") ??
        panel.current?.querySelector<HTMLElement>("input:not(:disabled), button:not(:disabled)");
      target?.focus();
    });
    return () => {
      window.cancelAnimationFrame(frame);
      window.requestAnimationFrame(() => {
        const resolved = resolveReturnFocus?.();
        const returnTarget = resolved?.isConnected ? resolved : anchor?.isConnected ? anchor : previous;
        returnTarget?.focus({ preventScroll: true });
      });
    };
  }, [anchor, modal, open, resolveReturnFocus]);
  useEffect(() => {
    if (!open || modal) {return;}
    const closeOutside = (event: PointerEvent) => {
      if (!(event.target instanceof Node) || panel.current?.contains(event.target) || anchor?.contains(event.target)) {return;}
      onClose();
    };
    document.addEventListener("pointerdown", closeOutside);
    return () => document.removeEventListener("pointerdown", closeOutside);
  }, [anchor, modal, onClose, open]);
  if (!open) {return null;}
  const dialog = <section
      className="app-dialog favorite-dialog"
      ref={panel}
      role={role}
      aria-modal={modal ? "true" : undefined}
      aria-labelledby={titleId}
      aria-describedby={description ? descriptionId : undefined}
      onKeyDown={(event) => {
        if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); onClose(); return; }
        if (!modal || event.key !== "Tab") {return;}
        const focusable = Array.from(panel.current?.querySelectorAll<HTMLElement>("button:not(:disabled), input:not(:disabled), select:not(:disabled), [href], [tabindex]:not([tabindex='-1'])") ?? []);
        if (!focusable.length) {return;}
        const first = focusable[0]; const last = focusable[focusable.length - 1];
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
      }}
    >
      <header className="favorite-dialog-head"><div><h2 id={titleId}>{title}</h2>{description ? <p id={descriptionId}>{description}</p> : null}</div>{dismissButton ? <button type="button" aria-label="关闭收藏夹管理" onClick={onClose}>×</button> : null}</header>
      {children}
    </section>;
  const layer = !modal
    ? <div className="favorite-popover">{dialog}</div>
    : <div className="dialog-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) {onClose();} }}>{dialog}</div>;
  return createPortal(layer, document.body);
}

export function FolderPickerDialog({
  open, title, folders, selectedFolderIds, busy, anchor, resolveReturnFocus, onSave, onCreate, onClose,
}: {
  open: boolean; title: string; folders: FavoriteFolder[]; selectedFolderIds: string[]; busy: boolean;
  anchor?: HTMLElement | null; resolveReturnFocus?: () => HTMLElement | null;
  onSave: (folderIds: string[]) => void; onCreate: () => void; onClose: () => void;
}) {
  if (!open) {return null;}
  return <OpenFolderPickerDialog key={selectedFolderIds.join("\u0000")} title={title} folders={folders} selectedFolderIds={selectedFolderIds} busy={busy} anchor={anchor} resolveReturnFocus={resolveReturnFocus} onSave={onSave} onCreate={onCreate} onClose={onClose} />;
}

function OpenFolderPickerDialog({
  title, folders, selectedFolderIds, busy, anchor, resolveReturnFocus, onSave, onCreate, onClose,
}: {
  title: string; folders: FavoriteFolder[]; selectedFolderIds: string[]; busy: boolean;
  anchor?: HTMLElement | null; resolveReturnFocus?: () => HTMLElement | null;
  onSave: (folderIds: string[]) => void; onCreate: () => void; onClose: () => void;
}) {
  const [selected, setSelected] = useState<Set<string>>(() => new Set(selectedFolderIds));
  const [query, setQuery] = useState("");
  const visible = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase("zh-CN");
    return normalized ? folders.filter((folder) => folder.name.toLocaleLowerCase("zh-CN").includes(normalized)) : folders;
  }, [folders, query]);
  return <DialogFrame open title={title} modal={!anchor} anchor={anchor} resolveReturnFocus={resolveReturnFocus} dismissButton onClose={onClose}>
    <label className="favorite-folder-search"><span className="sr-only">搜索收藏夹</span><input data-dialog-autofocus type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索收藏夹" /></label>
    <div className="favorite-folder-options">
      {visible.length ? visible.map((folder) => <label key={folder.folderId}>
        <input
          type="checkbox"
          checked={selected.has(folder.folderId)}
          onChange={(event) => setSelected((current) => {
            const next = new Set(current);
            if (event.target.checked) {next.add(folder.folderId);} else {next.delete(folder.folderId);}
            return next;
          })}
        />
        <span>{folder.name}</span><strong>{folder.visibleGameCount}</strong>
      </label>) : <p className="favorite-folder-none">没有匹配的收藏夹。</p>}
    </div>
    <button className="favorite-create-inline" type="button" disabled={busy} onClick={onCreate}>＋ 新建收藏夹</button>
    <div className="dialog-actions"><button className="button" type="button" disabled={busy} onClick={() => onSave([...selected])}>{busy ? "保存中…" : "完成"}</button></div>
  </DialogFrame>;
}

export function FolderNameDialog({
  open, title, initialName = "", submitLabel = "保存", busy, error, onSubmit, onClose,
}: {
  open: boolean; title: string; initialName?: string; submitLabel?: string; busy: boolean; error?: string;
  onSubmit: (name: string) => void; onClose: () => void;
}) {
  if (!open) {return null;}
  return <OpenFolderNameDialog title={title} initialName={initialName} submitLabel={submitLabel} busy={busy} error={error} onSubmit={onSubmit} onClose={onClose} />;
}

function OpenFolderNameDialog({
  title, initialName, submitLabel, busy, error, onSubmit, onClose,
}: {
  title: string; initialName: string; submitLabel: string; busy: boolean; error?: string;
  onSubmit: (name: string) => void; onClose: () => void;
}) {
  const [name, setName] = useState(initialName);
  return <DialogFrame open title={title} description="为收藏夹命名；之后可以随时重命名或删除，游戏本身不会受影响。" onClose={onClose}>
    <form onSubmit={(event) => { event.preventDefault(); if (name.trim()) {onSubmit(name);} }}>
      <label className="favorite-folder-name"><span>收藏夹名称</span><input data-dialog-autofocus value={name} maxLength={160} placeholder="例如：纵版街机" onChange={(event) => setName(event.target.value)} autoComplete="off" /></label>
      {error ? <p className="favorite-form-error" role="alert">{error}</p> : null}
      <div className="dialog-actions"><button className="button secondary" type="button" disabled={busy} onClick={onClose}>取消</button><button className="button" type="submit" disabled={busy || !name.trim()}>{busy ? "保存中…" : submitLabel}</button></div>
    </form>
  </DialogFrame>;
}

export function FolderEditDialog({
  open, initialName, busy, error, onSubmit, onDelete, onClose,
}: {
  open: boolean; initialName: string; busy: boolean; error?: string;
  onSubmit: (name: string) => void; onDelete: () => void; onClose: () => void;
}) {
  if (!open) {return null;}
  return <OpenFolderEditDialog initialName={initialName} busy={busy} error={error} onSubmit={onSubmit} onDelete={onDelete} onClose={onClose} />;
}

function OpenFolderEditDialog({
  initialName, busy, error, onSubmit, onDelete, onClose,
}: {
  initialName: string; busy: boolean; error?: string;
  onSubmit: (name: string) => void; onDelete: () => void; onClose: () => void;
}) {
  const [name, setName] = useState(initialName);
  return <DialogFrame open title="编辑收藏夹" description="重命名只改变收藏夹名称，不影响其中的游戏。" onClose={onClose}>
    <form onSubmit={(event) => { event.preventDefault(); if (name.trim()) {onSubmit(name);} }}>
      <label className="favorite-folder-name"><span>收藏夹名称</span><input data-dialog-autofocus value={name} maxLength={160} onChange={(event) => setName(event.target.value)} autoComplete="off" /></label>
      {error ? <p className="favorite-form-error" role="alert">{error}</p> : null}
      <div className="dialog-actions"><button className="button danger favorite-dialog-delete" type="button" disabled={busy} onClick={onDelete}>删除收藏夹…</button><button className="button secondary" type="button" disabled={busy} onClick={onClose}>取消</button><button className="button" type="submit" disabled={busy || !name.trim()}>{busy ? "保存中…" : "保存"}</button></div>
    </form>
  </DialogFrame>;
}
