"use client";

import { useEffect, useId, useRef, useState } from "react";
import { userStorageKey } from "@/features/auth/storage";
import { categoryForPlatform, directoryCategories, matchesDirectory, type DirectoryCategory, type DirectoryChoice } from "./directory-categories";

type DirectorySelectorProps = {
  directories: DirectoryChoice[];
  onSelect: (id: string) => void;
  reconfiguring: boolean;
  selectedId: string;
  userId?: string;
};

function readRecent(key: string | null): string[] {
  if (!key || typeof window === "undefined") {return [];}
  try {
    const value: unknown = JSON.parse(window.localStorage.getItem(key) ?? "[]");
    return Array.isArray(value) ? value.filter((id): id is string => typeof id === "string").slice(0, 3) : [];
  } catch {return [];}
}

function DirectoryRow({ directory, onChoose, selected }: { directory: DirectoryChoice; onChoose: (id: string) => void; selected: boolean }) {
  return <button
    className="import-directory-option"
    type="button"
    aria-pressed={selected}
    onClick={() => onChoose(directory.id)}
  >
    <strong>{directory.name}</strong>
    <small>{directory.platformName} · {directory.coreName}</small>
  </button>;
}

export function DirectorySelector({ directories, onSelect, reconfiguring, selectedId, userId }: DirectorySelectorProps) {
  const storageKey = userStorageKey(userId, "imports", "recent-directories");
  const labelId = useId();
  const buttonId = useId();
  const panelId = useId();
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [above, setAbove] = useState(false);
  const [query, setQuery] = useState("");
  const [expanded, setExpanded] = useState<DirectoryCategory | null>(null);
  const [recentState, setRecentState] = useState(() => ({ key: storageKey, ids: readRecent(storageKey) }));
  const recentIds = recentState.key === storageKey ? recentState.ids : readRecent(storageKey);
  const selected = directories.find((directory) => directory.id === selectedId);
  const recent = recentIds.map((id) => directories.find((directory) => directory.id === id))
    .filter((directory): directory is DirectoryChoice => Boolean(directory));
  const filtered = directories.filter((directory) => matchesDirectory(directory, query));

  useEffect(() => {if (open) {searchRef.current?.focus();}}, [open]);
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
    const next = [id, ...recentIds.filter((recentId) => recentId !== id)].slice(0, 3);
    setRecentState({ key: storageKey, ids: next });
    if (storageKey) {
      try {window.localStorage.setItem(storageKey, JSON.stringify(next));} catch { /* Storage may be unavailable. */ }
    }
    setOpen(false);
    setQuery("");
    buttonRef.current?.focus();
  }

  function toggle() {
    if (open) {setOpen(false); return;}
    const bounds = rootRef.current?.getBoundingClientRect();
    if (bounds) {setAbove(window.innerHeight - bounds.bottom < 340 && bounds.top > window.innerHeight - bounds.bottom);}
    setQuery("");
    setOpen(true);
  }

  return <div className="field import-directory-field" ref={rootRef} onBlurCapture={(event) => {
    if (!(event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget))) {setOpen(false);}
  }}>
    <span id={labelId} className="field-label">目标游戏目录</span>
    <button
      id={buttonId}
      ref={buttonRef}
      className={`import-directory-trigger${selected ? " has-selection" : ""}`}
      type="button"
      aria-labelledby={`${labelId} ${buttonId}`}
      aria-expanded={open}
      aria-controls={panelId}
      onClick={toggle}
    >{selected?.name ?? "请选择目标游戏目录"}<span aria-hidden="true">⌄</span></button>
    <small>{reconfiguring ? "可以保留原目录，也可以选择正确的平台目录后重新识别。" : "必须主动选择，避免将游戏导入到错误目录。"}</small>
    {open ? <div className={`import-directory-panel${above ? " is-above" : ""}`} id={panelId} role="region" aria-label="可选游戏目录">
      <input
        ref={searchRef}
        type="search"
        aria-label="搜索目录、平台或核心"
        autoComplete="off"
        placeholder="搜索目录、平台或核心…"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
      />
      <div className="import-directory-results">
        {query.trim() ? <>
          <p className="import-directory-section-label" role="status">找到 {filtered.length} 个目录</p>
          {filtered.map((directory) => <DirectoryRow key={directory.id} directory={directory} selected={directory.id === selectedId} onChoose={choose} />)}
          {!filtered.length ? <p className="import-directory-empty">没有匹配的游戏目录。可尝试平台或核心名称。</p> : null}
        </> : <>
          {recent.length ? <section aria-label="最近使用"><p className="import-directory-section-label">最近使用</p>{recent.map((directory) => <DirectoryRow key={directory.id} directory={directory} selected={directory.id === selectedId} onChoose={choose} />)}</section> : null}
          {directoryCategories.map((category) => {
            const items = directories.filter((directory) => categoryForPlatform(directory.platformId) === category.id);
            if (!items.length) {return null;}
            const groupId = `${panelId}-${category.id}`;
            const isExpanded = expanded === category.id;
            return <section className="import-directory-category" key={category.id}>
              <button type="button" aria-expanded={isExpanded} aria-controls={groupId} onClick={() => setExpanded(isExpanded ? null : category.id)}>
                <span aria-hidden="true">{isExpanded ? "⌄" : "›"}</span>{category.label}<small>{items.length}</small>
              </button>
              {isExpanded ? <div id={groupId} role="group" aria-label={category.label}>{items.map((directory) => <DirectoryRow key={directory.id} directory={directory} selected={directory.id === selectedId} onChoose={choose} />)}</div> : null}
            </section>;
          })}
        </>}
      </div>
    </div> : null}
  </div>;
}
