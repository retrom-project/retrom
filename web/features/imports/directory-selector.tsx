"use client";

import { useCallback, useEffect, useId, useRef, useState, type CSSProperties } from "react";
import { userStorageKey } from "@/features/auth/storage";
import { categoryForPlatform, directoryCategories, matchesDirectory, type DirectoryCategory, type DirectoryChoice } from "./directory-categories";

type DirectorySelectorProps = {
  collectionName?: string;
  directories: DirectoryChoice[];
  disabled?: boolean;
  onSelect: (id: string) => void;
  reconfiguring?: boolean;
  selectedId: string;
  userId?: string;
};

type FloatingPosition = { above: boolean; left: number; maxHeight: number; top: number; width: number };

function positionForTrigger(button: HTMLButtonElement): FloatingPosition {
  const rect = button.getBoundingClientRect();
  const viewportWidth = document.documentElement.clientWidth || window.innerWidth;
  const viewportHeight = document.documentElement.clientHeight || window.innerHeight;
  const margin = 8;
  const gap = 6;
  const preferredHeight = 320;
  const below = viewportHeight - rect.bottom - gap - margin;
  const above = rect.top - gap - margin;
  const placeAbove = below < Math.min(220, preferredHeight) && above > below;
  const width = Math.min(rect.width, viewportWidth - margin * 2);
  return {
    above: placeAbove,
    left: Math.min(Math.max(rect.left, margin), viewportWidth - width - margin),
    maxHeight: Math.max(80, Math.min(preferredHeight, placeAbove ? above : below)),
    top: placeAbove ? rect.top - gap : rect.bottom + gap,
    width,
  };
}

function floatingStyle(position: FloatingPosition | null): CSSProperties {
  if (!position) {return { visibility: "hidden" };}
  return {
    position: "fixed", left: position.left, top: position.top, width: position.width,
    maxHeight: position.maxHeight, transform: position.above ? "translateY(-100%)" : undefined,
  };
}

function readRecent(key: string | null): string[] {
  if (!key || typeof window === "undefined") {return [];}
  try {
    const value: unknown = JSON.parse(window.localStorage.getItem(key) ?? "[]");
    return Array.isArray(value) ? value.filter((id): id is string => typeof id === "string").slice(0, 3) : [];
  } catch {return [];}
}

function DirectoryRow({ directory, disabled, onChoose, selected }: { directory: DirectoryChoice; disabled: boolean; onChoose: (id: string) => void; selected: boolean }) {
  return <button
    className="import-directory-option"
    type="button"
    aria-pressed={selected}
    disabled={disabled}
    onClick={() => onChoose(directory.id)}
  >
    <strong>{directory.name}</strong>
    <small>{directory.platformName} · {directory.coreName}</small>
  </button>;
}

function DirectoryCategorySection({ category, directories, disabled, expanded, onChoose, onExpand, panelId, selectedId }: {
  category: (typeof directoryCategories)[number]; directories: DirectoryChoice[]; disabled: boolean; expanded: DirectoryCategory | null;
  onChoose: (id: string) => void; onExpand: (category: DirectoryCategory | null) => void; panelId: string; selectedId: string;
}) {
  const items = directories.filter((directory) => categoryForPlatform(directory.platformId) === category.id);
  if (!items.length) {return null;}
  const groupId = `${panelId}-${category.id}`;
  const isExpanded = expanded === category.id;
  return <section className="import-directory-category">
    <button type="button" aria-expanded={isExpanded} aria-controls={groupId} disabled={disabled} onClick={() => onExpand(isExpanded ? null : category.id)}>
      <svg className="import-directory-chevron" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
      {category.label}<small>{items.length}</small>
    </button>
    {isExpanded ? <div id={groupId} role="group" aria-label={category.label}>{items.map((directory) => <DirectoryRow key={directory.id} directory={directory} disabled={disabled} selected={directory.id === selectedId} onChoose={onChoose} />)}</div> : null}
  </section>;
}

function DirectoryResults({ directories, disabled, expanded, isCollectionMapping, onChoose, onExpand, panelId, query, recent, selectedId }: {
  directories: DirectoryChoice[]; disabled: boolean; expanded: DirectoryCategory | null; isCollectionMapping: boolean;
  onChoose: (id: string) => void; onExpand: (category: DirectoryCategory | null) => void; panelId: string;
  query: string; recent: DirectoryChoice[]; selectedId: string;
}) {
  const filtered = directories.filter((directory) => matchesDirectory(directory, query));
  return <div className="import-directory-results">
    {isCollectionMapping ? <button className="import-directory-skip" type="button" aria-pressed={selectedId === "SKIP"} disabled={disabled} onClick={() => onChoose("SKIP")}>跳过此集合</button> : null}
    {isCollectionMapping && selectedId ? <button className="import-directory-clear" type="button" disabled={disabled} onClick={() => onChoose("")}>清除处理方式</button> : null}
    {query.trim() ? <>
      <p className="import-directory-section-label" role="status">找到 {filtered.length} 个目录</p>
      {filtered.map((directory) => <DirectoryRow key={directory.id} directory={directory} disabled={disabled} selected={directory.id === selectedId} onChoose={onChoose} />)}
      {!filtered.length ? <p className="import-directory-empty">没有匹配的游戏目录。可尝试平台或核心名称。</p> : null}
    </> : <>
      {recent.length ? <section aria-label="最近使用"><p className="import-directory-section-label">最近使用</p>{recent.map((directory) => <DirectoryRow key={directory.id} directory={directory} disabled={disabled} selected={directory.id === selectedId} onChoose={onChoose} />)}</section> : null}
      {directoryCategories.map((category) => <DirectoryCategorySection key={category.id} category={category} directories={directories} disabled={disabled} expanded={expanded} onChoose={onChoose} onExpand={onExpand} panelId={panelId} selectedId={selectedId} />)}
    </>}
  </div>;
}

function selectedDirectoryLabel(selected: DirectoryChoice | undefined, selectedId: string, isCollectionMapping: boolean): string {
  if (selected) {return selected.name;}
  if (isCollectionMapping && selectedId === "SKIP") {return "跳过此集合";}
  return isCollectionMapping ? "请选择，不会自动映射" : "请选择目标游戏目录";
}

function recentDirectories(directories: DirectoryChoice[], ids: string[]): DirectoryChoice[] {
  return ids.map((id) => directories.find((directory) => directory.id === id))
    .filter((directory): directory is DirectoryChoice => Boolean(directory));
}

export function DirectorySelector({ collectionName, directories, disabled = false, onSelect, reconfiguring = false, selectedId, userId }: DirectorySelectorProps) {
  const isCollectionMapping = collectionName !== undefined;
  const storageKey = userStorageKey(userId, "imports", "recent-directories");
  const labelId = useId();
  const buttonId = useId();
  const panelId = useId();
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [above, setAbove] = useState(false);
  const [floatingPosition, setFloatingPosition] = useState<FloatingPosition | null>(null);
  const [query, setQuery] = useState("");
  const [expanded, setExpanded] = useState<DirectoryCategory | null>(null);
  const [recentState, setRecentState] = useState(() => ({ key: storageKey, ids: readRecent(storageKey) }));
  const recentIds = recentState.key === storageKey ? recentState.ids : readRecent(storageKey);
  const selected = directories.find((directory) => directory.id === selectedId);
  const selectedLabel = selectedDirectoryLabel(selected, selectedId, isCollectionMapping);
  const recent = recentDirectories(directories, recentIds);

  const updateFloatingPosition = useCallback(() => {
    if (buttonRef.current) {setFloatingPosition(positionForTrigger(buttonRef.current));}
  }, []);

  useEffect(() => {if (open) {searchRef.current?.focus();}}, [open]);
  useEffect(() => {
    if (!open || !isCollectionMapping) {return;}
    const button = buttonRef.current;
    const observer = button && typeof ResizeObserver !== "undefined" ? new ResizeObserver(updateFloatingPosition) : null;
    if (button) {observer?.observe(button);}
    window.addEventListener("resize", updateFloatingPosition);
    window.addEventListener("scroll", updateFloatingPosition, true);
    window.visualViewport?.addEventListener("resize", updateFloatingPosition);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", updateFloatingPosition);
      window.removeEventListener("scroll", updateFloatingPosition, true);
      window.visualViewport?.removeEventListener("resize", updateFloatingPosition);
    };
  }, [isCollectionMapping, open, updateFloatingPosition]);
  useEffect(() => {
    if (!open) {return;}
    function onPointerDown(event: PointerEvent) {
      if (event.target instanceof Node && !rootRef.current?.contains(event.target)) {setOpen(false);}
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {setOpen(false); buttonRef.current?.focus();}
    }
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  function choose(id: string) {
    onSelect(id);
    if (id && id !== "SKIP") {
      const next = [id, ...recentIds.filter((recentId) => recentId !== id)].slice(0, 3);
      setRecentState({ key: storageKey, ids: next });
      if (storageKey) {
        try {window.localStorage.setItem(storageKey, JSON.stringify(next));} catch { /* Storage may be unavailable. */ }
      }
    }
    setOpen(false);
    setQuery("");
    buttonRef.current?.focus();
  }

  function toggle() {
    if (open) {setOpen(false); return;}
    const bounds = rootRef.current?.getBoundingClientRect();
    if (bounds && !isCollectionMapping) {setAbove(window.innerHeight - bounds.bottom < 340 && bounds.top > window.innerHeight - bounds.bottom);}
    if (isCollectionMapping) {updateFloatingPosition();}
    setQuery("");
    setOpen(true);
  }

  return <div className={`field import-directory-field${isCollectionMapping ? " is-collection-mapping" : ""}`} ref={rootRef} onBlurCapture={(event) => {
    if (!(event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget))) {setOpen(false);}
  }}>
    <span id={labelId} className="field-label">{isCollectionMapping ? "处理方式" : "目标游戏目录"}</span>
    <button
      id={buttonId}
      ref={buttonRef}
      className={`import-directory-trigger${selected || selectedId === "SKIP" ? " has-selection" : ""}`}
      type="button"
      aria-label={isCollectionMapping ? `${collectionName} 处理方式` : undefined}
      aria-labelledby={isCollectionMapping ? undefined : `${labelId} ${buttonId}`}
      aria-expanded={open}
      aria-controls={panelId}
      disabled={disabled}
      onClick={toggle}
    >{selectedLabel}<svg className="import-directory-chevron" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg></button>
    {!isCollectionMapping ? <small>{reconfiguring ? "可以保留原目录，也可以选择正确的平台目录后重新识别。" : "必须主动选择，避免将游戏导入到错误目录。"}</small> : null}
    {open ? <div className={`import-directory-panel${above ? " is-above" : ""}`} id={panelId} role="region" aria-label="可选游戏目录" style={isCollectionMapping ? floatingStyle(floatingPosition) : undefined}>
      <input
        ref={searchRef}
        type="search"
        aria-label="搜索目录、平台或核心"
        autoComplete="off"
        placeholder="搜索目录、平台或核心…"
        value={query}
        disabled={disabled}
        onChange={(event) => setQuery(event.target.value)}
      />
      <DirectoryResults directories={directories} disabled={disabled} expanded={expanded} isCollectionMapping={isCollectionMapping} onChoose={choose} onExpand={setExpanded} panelId={panelId} query={query} recent={recent} selectedId={selectedId} />
    </div> : null}
  </div>;
}
