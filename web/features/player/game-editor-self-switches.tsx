"use client";

import {useCallback, useEffect, useRef, useState, type FormEvent, type UIEvent} from "react";
import type {RuntimeGameEditEventV1, RuntimeGameEditMapV1, RuntimeGameEditSelfSwitchKeyV1,
  RuntimeGameEditorV1} from "./runtime/contract";

type SelfSwitchEditor = NonNullable<RuntimeGameEditorV1["selfSwitches"]>;
const PAGE_SIZE = 40;
const KEYS: RuntimeGameEditSelfSwitchKeyV1[] = ["A", "B", "C", "D"];

export function GameEditorSelfSwitches({api, onNotice}: {
  api: SelfSwitchEditor; onNotice: () => void;
}) {
  const [currentMapId, setCurrentMapId] = useState(0);
  const [mapId, setMapId] = useState(0);
  const [mapName, setMapName] = useState("");
  const [mapOpen, setMapOpen] = useState(false);
  const [mapQuery, setMapQuery] = useState("");
  const [maps, setMaps] = useState<RuntimeGameEditMapV1[]>([]);
  const [mapNext, setMapNext] = useState<number | null>(null);
  const [mapBusy, setMapBusy] = useState(false);
  const [mapError, setMapError] = useState(false);
  const [events, setEvents] = useState<RuntimeGameEditEventV1[]>([]);
  const [eventNext, setEventNext] = useState<number | null>(null);
  const [eventBusy, setEventBusy] = useState(true);
  const [eventError, setEventError] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [saveError, setSaveError] = useState("");
  const mapSequence = useRef(0);
  const eventSequence = useRef(0);
  const pendingMapPage = useRef<number | null>(null);
  const pendingEventPage = useRef<number | null>(null);
  const selectedMap = useRef(0);
  const listRef = useRef<HTMLDivElement>(null);

  const loadMaps = useCallback(async (filter: string, offset: number) => {
    if (offset && pendingMapPage.current === offset) {return;}
    const sequence = offset ? mapSequence.current : ++mapSequence.current;
    pendingMapPage.current = offset || null;
    setMapBusy(true); setMapError(false);
    if (!offset) {setMaps([]); setMapNext(null);}
    try {
      const page = await api.maps(filter, offset, PAGE_SIZE);
      if (sequence !== mapSequence.current) {return;}
      if (!selectedMap.current) {selectedMap.current = page.currentMapId;}
      setCurrentMapId(page.currentMapId);
      setMapId((id) => id || page.currentMapId);
      setMapName((name) => name || page.currentMapName);
      setMaps((previous) => offset ? [...previous, ...page.maps] : page.maps);
      setMapNext(page.nextOffset);
    } catch {if (sequence === mapSequence.current) {setMapError(true);}}
    finally {if (sequence === mapSequence.current) {pendingMapPage.current = null; setMapBusy(false);}}
  }, [api]);

  const loadEvents = useCallback(async (selected: number, filter: string, offset: number) => {
    if (offset && pendingEventPage.current === offset) {return;}
    const sequence = offset ? eventSequence.current : ++eventSequence.current;
    pendingEventPage.current = offset || null;
    setEventBusy(true); setEventError(false); setSaveError("");
    if (!offset) {setEvents([]); setEventNext(null); if (listRef.current) {listRef.current.scrollTop = 0;}}
    try {
      const page = await api.events(selected, filter, offset, PAGE_SIZE);
      if (sequence !== eventSequence.current) {return;}
      setEvents((previous) => offset ? [...previous, ...page.events] : page.events);
      setEventNext(page.nextOffset);
    } catch {if (sequence === eventSequence.current) {setEventError(true);}}
    finally {if (sequence === eventSequence.current) {pendingEventPage.current = null; setEventBusy(false);}}
  }, [api]);

  useEffect(() => {
    let active = true;
    queueMicrotask(() => {if (active) {void loadMaps(mapQuery, 0);}});
    return () => {active = false;};
  }, [loadMaps, mapQuery]);
  useEffect(() => {
    let active = true;
    if (mapId) {queueMicrotask(() => {if (active) {void loadEvents(mapId, search, 0);}});}
    return () => {active = false;};
  }, [loadEvents, mapId, search]);
  useEffect(() => {
    const list = listRef.current;
    if (mapId && list && eventNext !== null && !eventBusy && !eventError &&
      list.scrollHeight <= list.clientHeight + 96) {void loadEvents(mapId, search, eventNext);}
  }, [mapId, search, eventNext, eventBusy, eventError, events, loadEvents]);

  function chooseMap(map: RuntimeGameEditMapV1) {
    selectedMap.current = map.id;
    setMapId(map.id); setMapName(map.label); setMapOpen(false); setMapQuery("");
    setSearch(""); setQuery(""); setSearchOpen(false); setSaveError("");
  }
  async function toggle(event: RuntimeGameEditEventV1, key: RuntimeGameEditSelfSwitchKeyV1) {
    try {
      const updated = await api.set(mapId, event.id, key, !event.switches[key]);
      if (selectedMap.current === mapId) {
        setEvents((rows) => rows.map((row) => row.id === updated.id ? updated : row));
        setSaveError("");
      }
      onNotice();
    } catch {if (selectedMap.current === mapId) {setSaveError(`${event.label} 的独立开关 ${key} 修改失败。`);}}
  }
  function mapScroll(event: UIEvent<HTMLDivElement>) {
    const list = event.currentTarget;
    if (mapNext !== null && !mapBusy && list.scrollTop + list.clientHeight >= list.scrollHeight - 80) {
      void loadMaps(mapQuery, mapNext);
    }
  }
  function eventScroll(event: UIEvent<HTMLDivElement>) {
    const list = event.currentTarget;
    if (eventNext !== null && !eventBusy && !eventError &&
      list.scrollTop + list.clientHeight >= list.scrollHeight - 96) {void loadEvents(mapId, search, eventNext);}
  }
  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const filter = query.trim();
    if (filter === search) {void loadEvents(mapId, filter, 0);} else {setSearch(filter);}
  }

  return <>
    <div className="game-editor-list-head"><h2>事件独立开关</h2><div><button className="button secondary" type="button" onClick={() => {setSearchOpen((open) => !open); if (searchOpen) {setQuery(""); setSearch("");}}}>{searchOpen ? "收起查找" : "查找"}</button><button className="button secondary" type="button" onClick={() => {if (mapId) {void loadEvents(mapId, search, 0);}}}>刷新</button></div></div>
    <GameEditorMapPicker mapName={mapName} mapId={mapId} currentMapId={currentMapId} open={mapOpen} setOpen={setMapOpen} query={mapQuery} setQuery={setMapQuery} maps={maps} busy={mapBusy} error={mapError} onScroll={mapScroll} onSelect={chooseMap} onRetry={() => void loadMaps(mapQuery, mapNext ?? 0)} />
    <p className="game-editor-category-help">每个事件分别拥有 A–D 四个开关；切换可能改变事件页面。</p>
    {searchOpen ? <form className="game-editor-self-switch-search" onSubmit={submitSearch}><input type="search" aria-label="按事件名称或编号查找" placeholder="按事件名称或编号查找" maxLength={80} value={query} onChange={(event) => setQuery(event.target.value)} /><button className="button secondary" type="submit">查找事件</button>{search ? <button className="button secondary" type="button" onClick={() => {setQuery(""); setSearch("");}}>清除</button> : null}</form> : null}
    {saveError ? <p className="game-editor-error" role="alert">{saveError}</p> : null}
    <div ref={listRef} className="game-editor-list game-editor-self-switch-list" aria-busy={eventBusy} onScroll={eventScroll}>{events.map((event) => <GameEditorSelfSwitchEvent key={event.id} event={event} onToggle={toggle} />)}{eventBusy && !events.length ? <p>正在读取事件…</p> : eventError ? <p role="alert">事件读取失败。<button className="button secondary" type="button" onClick={() => void loadEvents(mapId, search, events.length)}>重试</button></p> : !events.length ? <p>{search ? "没有找到匹配的事件。" : "这张地图没有事件。"}</p> : null}{eventBusy && events.length ? <p>正在加载后续事件…</p> : null}</div>
  </>;
}

function GameEditorMapPicker({mapName, mapId, currentMapId, open, setOpen, query, setQuery,
  maps, busy, error, onScroll, onSelect, onRetry}: {
  mapName: string; mapId: number; currentMapId: number; open: boolean;
  setOpen: (open: boolean | ((previous: boolean) => boolean)) => void;
  query: string; setQuery: (query: string) => void; maps: RuntimeGameEditMapV1[];
  busy: boolean; error: boolean; onScroll: (event: UIEvent<HTMLDivElement>) => void;
  onSelect: (map: RuntimeGameEditMapV1) => void; onRetry: () => void;
}) {
  return <div className="game-editor-map-picker"><span>地图</span><button className="button secondary" type="button" aria-expanded={open} aria-controls="game-editor-map-options" onClick={() => setOpen((value) => !value)}>{mapName || "读取中…"}{mapId === currentMapId ? " · 当前地图" : ""} ▾</button>
    {open ? <div id="game-editor-map-options" className="game-editor-map-options" onKeyDown={(event) => {if (event.key === "Escape") {event.stopPropagation(); setOpen(false);}}}><input type="search" aria-label="查找地图" placeholder="按地图名称或编号查找" value={query} onChange={(event) => setQuery(event.target.value)} /><div className="game-editor-map-results" onScroll={onScroll}>{maps.map((map) => <button key={map.id} className="button secondary" type="button" aria-label={`${map.label} · 地图 #${map.id}${map.id === currentMapId ? " · 当前" : ""}`} aria-current={map.id === mapId ? "true" : undefined} onClick={() => onSelect(map)}><span>{map.label}</span><small>地图 #{map.id}{map.id === currentMapId ? " · 当前" : ""}</small></button>)}{busy ? <p>正在读取地图…</p> : error ? <p role="alert">地图读取失败。<button className="button secondary" type="button" onClick={onRetry}>重试</button></p> : maps.length === 0 ? <p>没有找到地图。</p> : null}</div></div> : null}
  </div>;
}

function GameEditorSelfSwitchEvent({event, onToggle}: {
  event: RuntimeGameEditEventV1;
  onToggle: (event: RuntimeGameEditEventV1, key: RuntimeGameEditSelfSwitchKeyV1) => Promise<void>;
}) {
  const [pending, setPending] = useState<RuntimeGameEditSelfSwitchKeyV1 | null>(null);
  return <article className="game-editor-self-switch-event"><div><strong>{event.label}</strong><small>事件 #{event.id} · 坐标 {event.x}, {event.y}</small></div><div className="game-editor-self-switch-actions">{KEYS.map((key) => <button key={key} className={`button secondary${event.switches[key] ? " is-active" : ""}`} type="button" disabled={pending !== null} aria-label={`${event.label} 独立开关 ${key}，当前${event.switches[key] ? "开启" : "关闭"}，点击${event.switches[key] ? "关闭" : "开启"}`} onClick={() => {setPending(key); void onToggle(event, key).finally(() => setPending(null));}}>{key} · {event.switches[key] ? "开" : "关"}</button>)}</div></article>;
}
