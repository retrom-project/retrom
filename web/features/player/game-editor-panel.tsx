"use client";

import {useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent as ReactKeyboardEvent} from "react";
import type {RuntimeGameEditEntryV1, RuntimeGameEditorV1} from "./runtime/contract";
import {getActiveImmersiveGamepadIndex} from "@/features/immersive/active-gamepad";

type Category = {id: string; label: string};
const PAGE_SIZE = 40;

export function GameEditorPanel({editor, immersive, onClose}: {
  editor: RuntimeGameEditorV1; immersive?: boolean; onClose: () => void;
}) {
  const [categories, setCategories] = useState<Category[]>([]);
  const [category, setCategory] = useState("");
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [entries, setEntries] = useState<RuntimeGameEditEntryV1[]>([]);
  const [nextOffset, setNextOffset] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const closeRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const requestSequence = useRef(0);

  useEffect(() => {closeRef.current?.focus();}, []);
  useEffect(() => {
    let active = true;
    void editor.categories().then((result) => {
      if (!active) {return;}
      setCategories(result);
      setCategory(result[0]?.id ?? "");
      if (!result.length) {setLoading(false);}
    }).catch(() => {if (active) {setError("无法读取游戏修改项目，请进入游戏后重试。"); setLoading(false);}});
    return () => {active = false;};
  }, [editor]);

  const load = useCallback(async (selected: string, filter: string, page: number) => {
    const sequence = ++requestSequence.current;
    if (page === 0) {setLoading(true);} else {setLoadingMore(true);}
    setError("");
    try {
      const result = await editor.entries(selected, filter, page, PAGE_SIZE);
      if (sequence !== requestSequence.current) {return;}
      setEntries((current) => page === 0 ? result.entries : [...current, ...result.entries]);
      setNextOffset(result.nextOffset);
    } catch {
      if (sequence !== requestSequence.current) {return;}
      if (page === 0) {setEntries([]); setNextOffset(null);}
      setError("读取失败。请确认游戏已开始，再重试。");
    } finally {if (sequence === requestSequence.current) {setLoading(false); setLoadingMore(false);}}
  }, [editor]);

  useEffect(() => {
    let active = true;
    if (category) {queueMicrotask(() => {if (active) {void load(category, search, 0);}});}
    return () => {active = false;};
  }, [category, search, load]);
  useEffect(() => {
    if (immersive) {return;}
    const escape = (event: KeyboardEvent) => {if (event.key === "Escape") {event.preventDefault(); onClose();}};
    window.addEventListener("keydown", escape);
    return () => window.removeEventListener("keydown", escape);
  }, [immersive, onClose]);
  useEffect(() => {
    if (!immersive) {return;}
    const buttons = [0, 12, 13, 14, 15];
    const initialIndex = getActiveImmersiveGamepadIndex();
    const initialPad = initialIndex === null ? null : navigator.getGamepads?.()[initialIndex];
    let previous = buttons.map((button) => Boolean(initialPad?.buttons[button]?.pressed));
    const timer = window.setInterval(() => {
      const index = getActiveImmersiveGamepadIndex();
      const pad = index === null ? null : navigator.getGamepads?.()[index];
      if (!pad || !panelRef.current) {return;}
      const pressed = buttons.map((button) => Boolean(pad.buttons[button]?.pressed));
      const rising = pressed.map((value, button) => value && !previous[button]);
      previous = pressed;
      const focusable = Array.from(panelRef.current.querySelectorAll<HTMLElement>("button:not(:disabled), input:not(:disabled)"));
      const current = focusable.indexOf(document.activeElement as HTMLElement);
      if (rising[0]) {(document.activeElement as HTMLElement)?.click(); return;}
      if (rising[1] || rising[4]) {focusable[(current + focusable.length - 1) % focusable.length]?.focus();}
      if (rising[2] || rising[3]) {focusable[(current + 1) % focusable.length]?.focus();}
    }, 70);
    return () => window.clearInterval(timer);
  }, [immersive]);

  function chooseCategory(value: string) {
    requestSequence.current += 1;
    setCategory(value); setEntries([]); setLoading(true); setNextOffset(null);
    setQuery(""); setSearch(""); setSearchOpen(false); setNotice("");
  }

  async function save(entry: RuntimeGameEditEntryV1, value: number | string | boolean) {
    try {
      const updated = await editor.set(category, entry.id, value);
      setEntries((current) => current.map((item) => item.id === updated.id ? updated : item));
      setError("");
      setNotice(`${entry.label} 已修改。`);
      if (category === "actors") {void load(category, search, 0);}
    } catch {setError(`${entry.label} 修改失败；该值可能超出游戏允许的范围。`);}
  }
  const categoryLabel = categories.find((item) => item.id === category)?.label ?? "可修改";
  function submitSearch() {
    const value = query.trim();
    if (search === value) {void load(category, search, 0);} else {setSearch(value);}
  }
  function toggleSearch() {
    if (searchOpen && search) {setQuery(""); setSearch("");}
    setSearchOpen((open) => !open);
  }
  function trapFocus(event: ReactKeyboardEvent<HTMLElement>) {
    if (event.key !== "Tab" || !panelRef.current) {return;}
    const focusable = Array.from(panelRef.current.querySelectorAll<HTMLElement>("button:not(:disabled), input:not(:disabled)"));
    const first = focusable[0], last = focusable.at(-1);
    if (!first || !last) {return;}
    if (event.shiftKey && document.activeElement === first) {event.preventDefault(); last.focus();}
    else if (!event.shiftKey && document.activeElement === last) {event.preventDefault(); first.focus();}
  }

  return <section className={`game-editor-overlay${immersive ? " is-immersive" : ""}`} role="dialog" aria-modal="true" aria-labelledby="game-editor-title" onKeyDown={trapFocus}>
    <div ref={panelRef} className="game-editor-panel">
      <header className="game-editor-head"><div><small>当前游戏</small><h1 id="game-editor-title">游戏修改</h1></div><button ref={closeRef} className="button secondary" type="button" onClick={onClose}>返回游戏</button></header>
      <p className="game-editor-help">修改立即生效。离开前请创建存档，以保留修改后的进度。</p>
      <nav className="game-editor-categories" aria-label="修改类别">{categories.map((item) => <button key={item.id} type="button" className={`button secondary${item.id === category ? " is-active" : ""}`} aria-pressed={item.id === category} onClick={() => chooseCategory(item.id)}>{item.label}</button>)}</nav>
      {category ? <GameEditorToolbar label={categoryLabel} searchOpen={searchOpen} onSearch={toggleSearch} onRefresh={() => void load(category, search, 0)} /> : null}
      {searchOpen ? <GameEditorSearch query={query} hasFilter={Boolean(search)} onQuery={setQuery} onSubmit={submitSearch} onClear={() => {setQuery(""); setSearch("");}} /> : null}
      {error ? <p className="game-editor-error" role="alert">{error}</p> : null}
      {notice ? <p className="game-editor-notice" role="status">{notice}</p> : null}
      <GameEditorList category={category} categoryLabel={categoryLabel} entries={entries} loading={loading} filtered={Boolean(search)} onSave={save} />
      <GameEditorMore nextOffset={nextOffset} loading={loading || loadingMore} onMore={(page) => void load(category, search, page)} />
    </div>
  </section>;
}

function GameEditorToolbar({label, searchOpen, onSearch, onRefresh}: {
  label: string; searchOpen: boolean; onSearch: () => void; onRefresh: () => void;
}) {
  return <div className="game-editor-list-head"><h2>{label}</h2><div><button className="button secondary" type="button" onClick={onSearch} aria-expanded={searchOpen} aria-controls="game-editor-search">{searchOpen ? "收起查找" : "查找"}</button><button className="button secondary" type="button" onClick={onRefresh}>刷新</button></div></div>;
}

function GameEditorSearch({query, hasFilter, onQuery, onSubmit, onClear}: {
  query: string; hasFilter: boolean; onQuery: (value: string) => void; onSubmit: () => void; onClear: () => void;
}) {
  return <form id="game-editor-search" className="game-editor-search" onSubmit={(event: FormEvent<HTMLFormElement>) => {event.preventDefault(); onSubmit();}}><label htmlFor="game-editor-query">按名称查找</label><input id="game-editor-query" type="search" value={query} maxLength={80} onChange={(event) => onQuery(event.target.value)} /><button className="button secondary" type="submit">查找</button>{hasFilter ? <button className="button secondary" type="button" onClick={onClear}>清除</button> : null}</form>;
}

function GameEditorList({category, categoryLabel, entries, loading, filtered, onSave}: {
  category: string; categoryLabel: string; entries: RuntimeGameEditEntryV1[]; loading: boolean; filtered: boolean;
  onSave: (entry: RuntimeGameEditEntryV1, value: number | string | boolean) => Promise<void>;
}) {
  return <div className="game-editor-list" aria-busy={loading}>{loading ? <p>正在读取…</p> : entries.length
    ? entries.map((entry) => <GameEditorRow key={`${category}:${entry.id}:${String(entry.value)}`} entry={entry} onSave={(value) => onSave(entry, value)} />)
    : <p>{filtered ? "没有找到匹配的条目。" : `当前游戏没有${categoryLabel}条目。`}</p>}</div>;
}

function GameEditorMore({nextOffset, loading, onMore}: {nextOffset: number | null; loading: boolean; onMore: (page: number) => void}) {
  return nextOffset === null ? null : <footer className="game-editor-pagination"><button className="button secondary" type="button" disabled={loading} onClick={() => onMore(nextOffset)}>{loading ? "正在加载…" : "显示更多"}</button></footer>;
}

function GameEditorRow({entry, onSave}: {entry: RuntimeGameEditEntryV1; onSave: (value: number | string | boolean) => Promise<void>}) {
  const [draft, setDraft] = useState(String(entry.value));
  const [pending, setPending] = useState(false);
  const [invalid, setInvalid] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const numeric = Number(draft);
    const value = entry.valueType === "number" ? numeric : draft;
    if (entry.valueType === "number" && !validNumericDraft(entry, draft)) {
      setInvalid(`请输入 ${entry.min ?? "有效"} 至 ${entry.max ?? "有效"} 之间的整数。`); return;
    }
    setInvalid(""); setPending(true);
    try {await onSave(value);} finally {setPending(false);}
  }
  return <div className="game-editor-row"><div className="game-editor-row-label"><strong>{entry.label}</strong><small>当前：{displayEntryValue(entry)}</small></div>
    {entry.valueType === "boolean" ? <button className="button secondary" type="button" disabled={pending} aria-label={`${entry.label}，当前${entry.value ? "开启" : "关闭"}，点击切换`} onClick={() => {setPending(true); void onSave(!entry.value).finally(() => setPending(false));}}>{entry.value ? "关闭" : "开启"}</button>
      : entry.valueType === "unsupported" ? <span className="game-editor-unsupported">此值类型暂不支持</span>
        : <form className="game-editor-row-form" onSubmit={(event) => void submit(event)}>{entry.valueType === "number" ? <button className="button secondary" type="button" aria-label={`${entry.label}减一`} disabled={pending} onClick={() => setDraft(String(Math.max(entry.min ?? -999999999, Number(draft || 0) - 1)))}>−</button> : null}<input aria-label={`修改${entry.label}`} type={entry.valueType === "number" ? "number" : "text"} inputMode={entry.valueType === "number" ? "numeric" : undefined} step={entry.valueType === "number" ? 1 : undefined} min={entry.min} max={entry.max} maxLength={entry.valueType === "text" ? 500 : undefined} value={draft} onChange={(event) => setDraft(event.target.value)} />{entry.valueType === "number" ? <button className="button secondary" type="button" aria-label={`${entry.label}加一`} disabled={pending} onClick={() => setDraft(String(Math.min(entry.max ?? 999999999, Number(draft || 0) + 1)))}>+</button> : null}<button className="button secondary" type="submit" disabled={pending || draft === String(entry.value)}>{pending ? "保存中" : "应用"}</button>{invalid ? <small role="alert">{invalid}</small> : null}</form>}
  </div>;
}

function validNumericDraft(entry: RuntimeGameEditEntryV1, draft: string) {
  const number = Number(draft);
  return draft.trim() !== "" && Number.isSafeInteger(number) &&
    (entry.min === undefined || number >= entry.min) && (entry.max === undefined || number <= entry.max);
}

function displayEntryValue(entry: RuntimeGameEditEntryV1) {
  return entry.value === null ? "不支持" : typeof entry.value === "boolean" ? entry.value ? "开启" : "关闭" : String(entry.value);
}
