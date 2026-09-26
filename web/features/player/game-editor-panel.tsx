"use client";

import {useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent, type ReactNode, type Ref} from "react";
import type {RuntimeGameEditCategoryV1, RuntimeGameEditEntryV1, RuntimeGameEditorV1} from "./runtime/contract";
import {getActiveImmersiveGamepadIndex} from "@/features/immersive/active-gamepad";
import {GameEditorSelfSwitches} from "./game-editor-self-switches";

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
    if (activeCategory && category !== "self_switches") {queueMicrotask(() => {if (active) {void load(activeCategory, search, 0);}});}
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
      const sequence = requestSequence.current;
      const updated = await editor.set(activeCategory, entry.id, value);
      if (sequence !== requestSequence.current) {return;}
      setError("");
      setNotice({text: "已应用修改。离开前请创建存档。"});
      if (category === "classes") {
        setEntries((current) => current.map((item) => ({...item, value: item.id === updated.id})));
      } else if (category === "party") {
        try {
          const [page, refreshedCategories] = await Promise.all([
            editor.entries(activeCategory, search, 0, PAGE_SIZE), editor.categories(),
          ]);
          if (sequence === requestSequence.current) {
            setEntries(page.entries); setNextOffset(page.nextOffset); setCategories(refreshedCategories);
          }
        } catch {setError("修改已生效，但列表刷新失败。请点击刷新。");}
      } else {setEntries((current) => current.map((item) => item.id === updated.id ? updated : item));}
      if (category === "actors") {void load(activeCategory, search, 0);}
    } catch {setNotice(null); setError(`${entry.label} 修改失败；请检查游戏当前规则或允许的范围。`);}
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
      <GameEditorCategories categories={categories} active={category} onChoose={chooseCategory} />
      {category === "self_switches" ? <GameEditorSelfSwitchSection editor={editor} onNotice={() => setNotice({text: "已应用修改。离开前请创建存档。"})} /> : <>
      {category ? <GameEditorToolbar label={categoryLabel} searchOpen={searchOpen} onSearch={toggleSearch} onRefresh={() => {if (activeCategory) {void load(activeCategory, search, 0);}}} /> : null}
      {groups ? <GameEditorActorTabs groups={groups} activeCategory={activeCategory} onChoose={chooseGroup} /> : null}
      <GameEditorCategoryHelp category={category} />
      {searchOpen ? <GameEditorSearch query={query} hasFilter={Boolean(search)} onQuery={setQuery} onSubmit={submitSearch} onClear={() => {setQuery(""); changeSearch("");}} /> : null}
      {error ? <p className="game-editor-error" role="alert">{error}</p> : null}
      <GameEditorList listRef={listRef} grouped={Boolean(groups)} noGroup={Boolean(groups && !groups.length)} category={category} categoryLabel={categoryLabel} entries={entries} loading={loading} showLoading={showLoading} loadingMore={loadingMore} filtered={Boolean(search)} onSave={save}>
        <GameEditorMore markerRef={moreRef} nextOffset={loading ? null : nextOffset} loading={loadingMore} error={pageError} onRetry={() => {
          if (nextOffset === null) {return;}
          requestedPage.current = null;
          void load(activeCategory, search, nextOffset);
        }} />
      </GameEditorList>
      </>}
    </div>
  </section>;
}

function resolveActiveCategory(category: string, groups: RuntimeGameEditCategoryV1["groups"], selectedGroup: string) {
  return groups ? selectedGroup || groups[0]?.id || "" : category;
}

function GameEditorSelfSwitchSection({editor, onNotice}: {editor: RuntimeGameEditorV1; onNotice: () => void}) {
  return editor.selfSwitches ? <GameEditorSelfSwitches api={editor.selfSwitches} onNotice={onNotice} />
    : <p className="game-editor-error" role="alert">当前游戏无法读取事件独立开关。</p>;
}

function GameEditorCategoryHelp({category}: {category: string}) {
  const help: Record<string, string> = {
    skills: "这里显示角色主动学习的技能；职业或装备附加的技能仍可能可用。",
    states: "战斗不能由生命值控制；免疫状态可能无法添加。",
    classes: "转职可能改变等级与可穿戴装备，按游戏规则立即生效。",
  };
  return help[category] ? <p className="game-editor-category-help">{help[category]}</p> : null;
}

function GameEditorCategories({categories, active, onChoose}: {
  categories: RuntimeGameEditCategoryV1[]; active: string; onChoose: (id: string) => void;
}) {
  const scrollAreaRef = useRef<HTMLDivElement>(null);
  const navRef = useRef<HTMLElement>(null);
  const railRef = useRef<HTMLDivElement>(null);
  const dragOffset = useRef<number | null>(null);
  const [scroll, setScroll] = useState({viewport: 0, content: 0, left: 0});

  useEffect(() => {
    const nav = navRef.current;
    const scrollArea = scrollAreaRef.current;
    if (!nav || !scrollArea) {return;}
    const measure = () => setScroll({viewport: nav.clientWidth, content: nav.scrollWidth, left: nav.scrollLeft});
    const resize = () => {
      const selected = nav.querySelector<HTMLButtonElement>('button[aria-pressed="true"]');
      if (selected) {
        const bounds = nav.getBoundingClientRect(), button = selected.getBoundingClientRect();
        if (button.right > bounds.right) {nav.scrollLeft += button.right - bounds.right;}
        else if (button.left < bounds.left) {nav.scrollLeft -= bounds.left - button.left;}
      }
      measure();
    };
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(resize);
    observer?.observe(nav);
    nav.addEventListener("scroll", measure, {passive: true});
    window.addEventListener("resize", resize);
    const stopWheel = scrollHorizontallyOnWheel(scrollArea, nav);
    resize();
    return () => {observer?.disconnect(); nav.removeEventListener("scroll", measure); window.removeEventListener("resize", resize); stopWheel();};
  }, [categories, active]);

  const thumbWidth = scroll.content ? Math.max(24, scroll.viewport * scroll.viewport / scroll.content) : 0;
  const thumbLeft = scroll.content > scroll.viewport
    ? scroll.left / (scroll.content - scroll.viewport) * (scroll.viewport - thumbWidth) : 0;
  function moveTo(event: ReactPointerEvent<HTMLDivElement>, offset: number) {
    const nav = navRef.current, rail = railRef.current;
    if (!nav || !rail) {return;}
    const travel = rail.clientWidth - thumbWidth;
    if (travel <= 0) {return;}
    const position = Math.max(0, Math.min(travel, event.clientX - rail.getBoundingClientRect().left - offset));
    nav.scrollLeft = position / travel * (nav.scrollWidth - nav.clientWidth);
  }
  function startDrag(event: ReactPointerEvent<HTMLDivElement>) {
    if (event.button !== 0 || !railRef.current?.firstElementChild) {return;}
    const thumb = railRef.current.firstElementChild;
    dragOffset.current = event.target === thumb
      ? event.clientX - thumb.getBoundingClientRect().left : thumbWidth / 2;
    event.currentTarget.setPointerCapture(event.pointerId);
    moveTo(event, dragOffset.current);
  }

  return <div ref={scrollAreaRef} className="game-editor-category-scroll">
    <nav ref={navRef} className="game-editor-categories" aria-label="修改类别">{categories.map((item) => <button key={item.id} type="button" className={`button secondary${item.id === active ? " is-active" : ""}`} aria-pressed={item.id === active} onClick={() => onChoose(item.id)}>{item.label}</button>)}</nav>
    {scroll.content > scroll.viewport ? <div ref={railRef} className="game-editor-category-scrollbar" aria-hidden="true" onPointerDown={startDrag} onPointerMove={(event) => {if (dragOffset.current !== null) {moveTo(event, dragOffset.current);}}} onPointerUp={() => {dragOffset.current = null;}} onLostPointerCapture={() => {dragOffset.current = null;}}><span style={{width: thumbWidth, transform: `translateX(${thumbLeft}px)`}} /></div> : null}
  </div>;
}

function GameEditorActorTabs({groups, activeCategory, onChoose}: {
  groups: NonNullable<RuntimeGameEditCategoryV1["groups"]>;
  activeCategory: string;
  onChoose: (id: string) => void;
}) {
  const tabsRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const tabs = tabsRef.current;
    return tabs ? scrollHorizontallyOnWheel(tabs, tabs) : undefined;
  }, []);
  return <div ref={tabsRef} className="game-editor-actor-tabs" role="tablist" aria-label="选择人物">{groups.map((group, index) => <button key={group.id} className="button secondary" type="button" role="tab" aria-selected={group.id === activeCategory} aria-controls="game-editor-group-list" tabIndex={group.id === activeCategory ? 0 : -1} onClick={() => onChoose(group.id)} onKeyDown={(event) => {
    const direction = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
    const next = event.key === "Home" ? 0 : event.key === "End" ? groups.length - 1
      : direction ? (index + direction + groups.length) % groups.length : -1;
    if (next < 0) {return;}
    event.preventDefault(); onChoose(groups[next].id);
    tabsRef.current?.querySelectorAll<HTMLButtonElement>("button")[next]?.focus();
  }}>{group.label}</button>)}</div>;
}

function scrollHorizontallyOnWheel(area: HTMLElement, scroller: HTMLElement) {
  const onWheel = (event: WheelEvent) => {
    const max = scroller.scrollWidth - scroller.clientWidth;
    if (event.ctrlKey || max <= 0) {return;}
    const delta = Math.abs(event.deltaX) > Math.abs(event.deltaY) ? event.deltaX : event.deltaY;
    const scale = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? scroller.clientWidth : 1;
    const next = Math.max(0, Math.min(max, scroller.scrollLeft + delta * scale));
    if (next === scroller.scrollLeft) {return;}
    scroller.scrollLeft = next;
    event.preventDefault();
  };
  area.addEventListener("wheel", onWheel, {passive: false});
  return () => area.removeEventListener("wheel", onWheel);
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

type GameEditorRowProps = {
  category: string; entry: RuntimeGameEditEntryV1;
  onSave: (value: number | string | boolean) => Promise<void>;
};

function GameEditorRow(props: GameEditorRowProps) {
  if (props.category === "party" && props.entry.valueType === "number") {return <GameEditorPartyRow {...props} />;}
  if (props.entry.valueType === "boolean") {return <GameEditorBooleanRow {...props} />;}
  return <GameEditorScalarRow {...props} />;
}

function GameEditorRowLabel({category, entry}: Pick<GameEditorRowProps, "category" | "entry">) {
  return <div className="game-editor-row-label"><strong>{entry.label}</strong><small>当前：{displayEntryValue(entry, category)}</small></div>;
}

function GameEditorPartyRow({category, entry, onSave}: GameEditorRowProps) {
  const [pending, setPending] = useState(false);
  const position = Number(entry.value);
  function act(value: number) {setPending(true); void onSave(value).finally(() => setPending(false));}
  return <div className="game-editor-row"><GameEditorRowLabel category={category} entry={entry} />
    <div className="game-editor-party-actions">{position === 0
      ? <button className="button secondary" type="button" disabled={pending} aria-label={`${entry.label}加入队伍`} onClick={() => act(entry.max ?? 1)}>加入</button>
      : <><button className="button secondary" type="button" disabled={pending || position <= 1} aria-label={`${entry.label}上移`} onClick={() => act(position - 1)}>上移</button><button className="button secondary" type="button" disabled={pending || position >= (entry.max ?? 1)} aria-label={`${entry.label}下移`} onClick={() => act(position + 1)}>下移</button><button className="button secondary" type="button" disabled={pending || (entry.max ?? 1) <= 1} aria-label={`${entry.label}移出队伍`} onClick={() => act(0)}>移出</button></>}
    </div>
  </div>;
}

function GameEditorBooleanRow({category, entry, onSave}: GameEditorRowProps) {
  const [pending, setPending] = useState(false);
  const text = booleanControlText(category, Boolean(entry.value));
  const selectedClass = category === "classes" && entry.value === true;
  return <div className="game-editor-row"><GameEditorRowLabel category={category} entry={entry} />
    <button className="button secondary" type="button" disabled={pending || selectedClass}
      aria-label={`${entry.label}，当前${text.state}${selectedClass ? "" : `，点击${text.verb}`}`}
      onClick={() => {setPending(true); void onSave(category === "classes" ? true : !entry.value).finally(() => setPending(false));}}>{text.action}</button>
  </div>;
}

function GameEditorScalarRow({category, entry, onSave}: GameEditorRowProps) {
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
  return <div className="game-editor-row"><GameEditorRowLabel category={category} entry={entry} />
    {entry.valueType === "unsupported" ? <span className="game-editor-unsupported">此值类型暂不支持</span>
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
  if (category === "party" && typeof entry.value === "number") {
    return entry.value ? `队伍第 ${entry.value} 位` : "未入队";
  }
  if (typeof entry.value === "boolean") {
    return booleanControlText(category, entry.value).state;
  }
  return String(entry.value);
}

function booleanControlText(category: string, value: boolean) {
  if (category === "skills") {
    return {state: value ? "已学会" : "未学会", action: value ? "遗忘" : "学习", verb: value ? "遗忘" : "学习"};
  }
  if (category === "states") {
    return {state: value ? "生效中" : "未生效", action: value ? "移除" : "添加", verb: value ? "移除" : "添加"};
  }
  if (category === "classes") {
    return {state: value ? "当前职业" : "可转职", action: value ? "已选" : "转职", verb: "转职"};
  }
  return {state: value ? "开启" : "关闭", action: value ? "关闭" : "开启", verb: "切换"};
}
