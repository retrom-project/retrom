"use client";

import {useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent as ReactKeyboardEvent, type ReactNode, type Ref} from "react";
import type {RuntimeGameEditCategoryV1, RuntimeGameEditEntryV1, RuntimeGameEditorV1} from "./runtime/contract";
import {getActiveImmersiveGamepadIndex} from "@/features/immersive/active-gamepad";

const PAGE_SIZE = 40;

export function GameEditorPanel({editor, immersive, onClose}: {
  editor: RuntimeGameEditorV1; immersive?: boolean; onClose: () => void;
}) {
  const [categories, setCategories] = useState<RuntimeGameEditCategoryV1[]>([]);
  const [category, setCategory] = useState("");
  const [selectedGroup, setSelectedGroup] = useState("");
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [entries, setEntries] = useState<RuntimeGameEditEntryV1[]>([]);
  const [nextOffset, setNextOffset] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [showLoading, setShowLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [pageError, setPageError] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState<{text: string} | null>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const moreRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const requestSequence = useRef(0);
  const requestedPage = useRef<string | null>(null);
  const selectedCategory = categories.find((item) => item.id === category);
  const groups = selectedCategory?.groups;
  const activeCategory = resolveActiveCategory(category, groups, selectedGroup);

  useEffect(() => {closeRef.current?.focus();}, []);
  useEffect(() => {
    if (!notice) {return;}
    const timer = window.setTimeout(() => setNotice(null), 3200);
    return () => window.clearTimeout(timer);
  }, [notice]);
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
    const loadingTimer = page === 0 ? window.setTimeout(() => {
      if (sequence === requestSequence.current) {setShowLoading(true);}
    }, 180) : null;
    if (page === 0) {requestedPage.current = null; setLoading(true); setShowLoading(false);} else {setLoadingMore(true);}
    setPageError(false);
    setError("");
    try {
      const result = await editor.entries(selected, filter, page, PAGE_SIZE);
      if (sequence !== requestSequence.current) {return;}
      setEntries((current) => page === 0 ? result.entries : [...current, ...result.entries]);
      setNextOffset(result.nextOffset);
    } catch {
      if (sequence !== requestSequence.current) {return;}
      if (page === 0) {
        setEntries([]); setNextOffset(null);
        setError("读取失败。请确认游戏已开始，再重试。");
      } else {setPageError(true);}
    } finally {
      if (loadingTimer !== null) {window.clearTimeout(loadingTimer);}
      if (sequence === requestSequence.current) {setLoading(false); setShowLoading(false); setLoadingMore(false);}
    }
  }, [editor]);

  useEffect(() => {
    let active = true;
    if (activeCategory) {queueMicrotask(() => {if (active) {void load(activeCategory, search, 0);}});}
    else if (category) {queueMicrotask(() => {if (active) {setLoading(false);}});}
    return () => {active = false;};
  }, [activeCategory, category, search, load]);
  useEffect(() => {
    const marker = moreRef.current;
    const list = marker?.parentElement;
    if (!marker || !list || !activeCategory || nextOffset === null || loading || loadingMore || pageError) {return;}
    const observedSequence = requestSequence.current;
    const observer = new IntersectionObserver(([entry]) => {
      if (!entry?.isIntersecting || observedSequence !== requestSequence.current) {return;}
      const key = JSON.stringify([activeCategory, search, nextOffset]);
      if (requestedPage.current === key) {return;}
      requestedPage.current = key;
      void load(activeCategory, search, nextOffset);
    }, {root: list, rootMargin: "0px 0px 96px 0px"});
    observer.observe(marker);
    return () => observer.disconnect();
  }, [activeCategory, search, nextOffset, loading, loadingMore, pageError, load]);
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
    setCategory(value); setSelectedGroup(""); setEntries([]); setLoading(true); setShowLoading(false); setNextOffset(null);
    requestedPage.current = null; if (listRef.current) {listRef.current.scrollTop = 0;}
    setQuery(""); setSearch(""); setSearchOpen(false); setNotice(null);
  }
  function chooseGroup(value: string) {
    if (value === activeCategory) {return;}
    requestSequence.current += 1;
    requestedPage.current = null;
    setSelectedGroup(value); setEntries([]); setNextOffset(null); setLoading(true); setShowLoading(false);
    setQuery(""); setSearch(""); setSearchOpen(false); setNotice(null);
    if (listRef.current) {listRef.current.scrollTop = 0;}
  }

  async function save(entry: RuntimeGameEditEntryV1, value: number | string | boolean) {
    try {
      const updated = await editor.set(activeCategory, entry.id, value);
      setEntries((current) => current.map((item) => item.id === updated.id ? updated : item));
      setError("");
      setNotice({text: "已应用修改。离开前请创建存档。"});
      if (category === "actors") {void load(activeCategory, search, 0);}
    } catch {setNotice(null); setError(`${entry.label} 修改失败；该值可能超出游戏允许的范围。`);}
  }
  const categoryLabel = selectedCategory?.label ?? "可修改";
  function changeSearch(value: string) {
    requestSequence.current += 1;
    requestedPage.current = null;
    setEntries([]); setNextOffset(null); setLoading(true); setShowLoading(false); setSearch(value);
  }
  function submitSearch() {
    const value = query.trim();
    if (search === value) {void load(activeCategory, search, 0);} else {changeSearch(value);}
  }
  function toggleSearch() {
    if (searchOpen && search) {setQuery(""); changeSearch("");}
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
      <div className="game-editor-feedback"><p className="game-editor-help" aria-hidden={Boolean(notice)}>修改立即生效。离开前请创建存档，以保留修改后的进度。</p>{notice ? <p className="game-editor-notice" role="status">{notice.text}</p> : null}</div>
      <nav className="game-editor-categories" aria-label="修改类别">{categories.map((item) => <button key={item.id} type="button" className={`button secondary${item.id === category ? " is-active" : ""}`} aria-pressed={item.id === category} onClick={() => chooseCategory(item.id)}>{item.label}</button>)}</nav>
      {category ? <GameEditorToolbar label={categoryLabel} searchOpen={searchOpen} onSearch={toggleSearch} onRefresh={() => {if (activeCategory) {void load(activeCategory, search, 0);}}} /> : null}
      {groups ? <GameEditorActorTabs groups={groups} activeCategory={activeCategory} onChoose={chooseGroup} /> : null}
      {category === "skills" ? <p className="game-editor-skill-help">这里显示角色主动学习的技能；职业或装备附加的技能仍可能可用。</p> : null}
      {searchOpen ? <GameEditorSearch query={query} hasFilter={Boolean(search)} onQuery={setQuery} onSubmit={submitSearch} onClear={() => {setQuery(""); changeSearch("");}} /> : null}
      {error ? <p className="game-editor-error" role="alert">{error}</p> : null}
      <GameEditorList listRef={listRef} grouped={Boolean(groups)} noGroup={Boolean(groups && !groups.length)} category={category} categoryLabel={categoryLabel} entries={entries} loading={loading} showLoading={showLoading} loadingMore={loadingMore} filtered={Boolean(search)} onSave={save}>
        <GameEditorMore markerRef={moreRef} nextOffset={loading ? null : nextOffset} loading={loadingMore} error={pageError} onRetry={() => {
          if (nextOffset === null) {return;}
          requestedPage.current = null;
          void load(activeCategory, search, nextOffset);
        }} />
      </GameEditorList>
    </div>
  </section>;
}

function resolveActiveCategory(category: string, groups: RuntimeGameEditCategoryV1["groups"], selectedGroup: string) {
  return groups ? selectedGroup || groups[0]?.id || "" : category;
}

function GameEditorActorTabs({groups, activeCategory, onChoose}: {
  groups: NonNullable<RuntimeGameEditCategoryV1["groups"]>;
  activeCategory: string;
  onChoose: (id: string) => void;
}) {
  const tabsRef = useRef<HTMLDivElement>(null);
  return <div ref={tabsRef} className="game-editor-actor-tabs" role="tablist" aria-label="选择人物">{groups.map((group, index) => <button key={group.id} className="button secondary" type="button" role="tab" aria-selected={group.id === activeCategory} aria-controls="game-editor-group-list" tabIndex={group.id === activeCategory ? 0 : -1} onClick={() => onChoose(group.id)} onKeyDown={(event) => {
    const direction = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
    const next = event.key === "Home" ? 0 : event.key === "End" ? groups.length - 1
      : direction ? (index + direction + groups.length) % groups.length : -1;
    if (next < 0) {return;}
    event.preventDefault(); onChoose(groups[next].id);
    tabsRef.current?.querySelectorAll<HTMLButtonElement>("button")[next]?.focus();
  }}>{group.label}</button>)}</div>;
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

function GameEditorList({listRef, grouped, noGroup, category, categoryLabel, entries, loading, showLoading, loadingMore, filtered, onSave, children}: {
  listRef: Ref<HTMLDivElement>; grouped: boolean; noGroup: boolean; category: string; categoryLabel: string; entries: RuntimeGameEditEntryV1[]; loading: boolean; showLoading: boolean; loadingMore: boolean; filtered: boolean;
  onSave: (entry: RuntimeGameEditEntryV1, value: number | string | boolean) => Promise<void>;
  children: ReactNode;
}) {
  return <div ref={listRef} id={grouped ? "game-editor-group-list" : undefined} role={grouped ? "tabpanel" : undefined} className="game-editor-list" aria-busy={loading || loadingMore}>{loading ? showLoading ? <p>正在读取…</p> : null : noGroup ? <p>当前队伍没有可修改的角色。</p> : entries.length
    ? entries.map((entry) => <GameEditorRow key={`${category}:${entry.id}:${String(entry.value)}`} category={category} entry={entry} onSave={(value) => onSave(entry, value)} />)
    : <p>{filtered ? "没有找到匹配的条目。" : `当前游戏没有${categoryLabel}条目。`}</p>}{children}</div>;
}

function GameEditorMore({markerRef, nextOffset, loading, error, onRetry}: {
  markerRef: Ref<HTMLDivElement>; nextOffset: number | null; loading: boolean; error: boolean; onRetry: () => void;
}) {
  return nextOffset === null ? null : <div ref={markerRef} className="game-editor-more">
    {loading ? <span role="status">正在加载后续项目…</span> : error
      ? <span role="alert">后续项目读取失败。<button className="button secondary" type="button" onClick={onRetry}>重试</button></span> : null}
  </div>;
}

function GameEditorRow({category, entry, onSave}: {
  category: string; entry: RuntimeGameEditEntryV1;
  onSave: (value: number | string | boolean) => Promise<void>;
}) {
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
  const booleanText = booleanControlText(category, Boolean(entry.value));
  return <div className="game-editor-row"><div className="game-editor-row-label"><strong>{entry.label}</strong><small>当前：{displayEntryValue(entry, category)}</small></div>
    {entry.valueType === "boolean" ? <button className="button secondary" type="button" disabled={pending} aria-label={`${entry.label}，当前${booleanText.state}，点击${booleanText.verb}`} onClick={() => {setPending(true); void onSave(!entry.value).finally(() => setPending(false));}}>{booleanText.action}</button>
      : entry.valueType === "unsupported" ? <span className="game-editor-unsupported">此值类型暂不支持</span>
        : <form className="game-editor-row-form" onSubmit={(event) => void submit(event)}>{entry.valueType === "number" ? <button className="button secondary" type="button" aria-label={`${entry.label}减一`} disabled={pending} onClick={() => setDraft(String(Math.max(entry.min ?? -999999999, Number(draft || 0) - 1)))}>−</button> : null}<input aria-label={`修改${entry.label}`} type={entry.valueType === "number" ? "number" : "text"} inputMode={entry.valueType === "number" ? "numeric" : undefined} step={entry.valueType === "number" ? 1 : undefined} min={entry.min} max={entry.max} maxLength={entry.valueType === "text" ? 500 : undefined} value={draft} onChange={(event) => setDraft(event.target.value)} />{entry.valueType === "number" ? <button className="button secondary" type="button" aria-label={`${entry.label}加一`} disabled={pending} onClick={() => setDraft(String(Math.min(entry.max ?? 999999999, Number(draft || 0) + 1)))}>+</button> : null}<button className="button secondary" type="submit" disabled={pending || draft === String(entry.value)}>{pending ? "保存中" : "应用"}</button>{invalid ? <small role="alert">{invalid}</small> : null}</form>}
  </div>;
}

function validNumericDraft(entry: RuntimeGameEditEntryV1, draft: string) {
  const number = Number(draft);
  return draft.trim() !== "" && Number.isSafeInteger(number) &&
    (entry.min === undefined || number >= entry.min) && (entry.max === undefined || number <= entry.max);
}

function displayEntryValue(entry: RuntimeGameEditEntryV1, category: string) {
  if (entry.value === null) {return "不支持";}
  if (typeof entry.value === "boolean") {
    return category === "skills" ? entry.value ? "已学会" : "未学会" : entry.value ? "开启" : "关闭";
  }
  return String(entry.value);
}

function booleanControlText(category: string, value: boolean) {
  if (category === "skills") {
    return {state: value ? "已学会" : "未学会", action: value ? "遗忘" : "学习", verb: value ? "遗忘" : "学习"};
  }
  return {state: value ? "开启" : "关闭", action: value ? "关闭" : "开启", verb: "切换"};
}
