"use client";
import { useCallback, useEffect, useId, useRef, useState } from "react";
import type { KeyboardEvent } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import type { Schema } from "@/lib/api/types";

const pageSize = 30;
export function ROMSetSelector({ gameId, coreId, entryFile, value, datSHA256, onSelect }: {
  gameId: string; coreId: string; entryFile: string; value?: string; datSHA256: string;
  onSelect: (name: string | undefined) => void;
}) {
  const id = useId(), input = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const search = useROMSetSearch({ gameId, coreId, entryFile, value, datSHA256 });
  const { text, setText, active, setActive, entries, selected, display, invalid } = search;
  useEffect(() => { input.current?.setCustomValidity(invalid ? "所选 ROM 集不在当前核心的 DAT 中，请重新选择。" : ""); }, [invalid]);
  useEffect(() => {
    if (open) { document.getElementById(`${id}-${active}`)?.scrollIntoView?.({ block: "nearest" }); }
  }, [open, active, id]);
  function choose(name: string | undefined) { onSelect(name); setOpen(false); setText(""); }
  function keydown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Escape") { event.preventDefault(); setOpen(false); setText(""); }
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault(); setOpen(true);
      setActive(index => Math.max(0, Math.min(entries.length - 1, index + (event.key === "ArrowDown" ? 1 : -1))));
    }
    if (event.key === "Enter" && open) { event.preventDefault(); if (entries[active]) { choose(entries[active].name); } }
  }
  return <div className="rom-set-selector" onBlur={event => {
    if (!event.currentTarget.contains(event.relatedTarget)) { setOpen(false); setText(""); }
  }}>
    <label className="field">ROM 集（DAT）
      <input ref={input} role="combobox" aria-autocomplete="list" aria-expanded={open} aria-controls={id}
        aria-activedescendant={open && entries[active] ? `${id}-${active}` : undefined}
        aria-invalid={invalid} title={display} value={open ? text : display} placeholder="搜索 ROM 集名称、游戏名称或版本"
        onFocus={() => { setOpen(true); setText(""); setActive(0); }} onClick={() => setOpen(true)}
        onChange={event => { setText(event.target.value); setOpen(true); }} onKeyDown={keydown} />
    </label>
    {open ? <ROMSetList id={id} entries={entries} active={active} selected={selected?.name} busy={search.busy}
      error={search.failure}
      total={search.total} offset={search.offset} onPage={offset => { search.setOffset(offset); setActive(0); }} onChoose={choose} /> : null}
    <small>选择仅对当前核心生效。更换 ROM 集后请先保存，再检查或上传 Parent；CRC 差异由人工审核。</small>
    {value ? <button type="button" className="button secondary" onClick={() => choose(undefined)}>按文件名重新匹配</button> : null}
    {invalid ? <p role="alert">所选 ROM 集“{value}”不在当前核心的 DAT 中，请重新选择。</p> : null}
    {search.failure ? <p role="alert">无法读取当前核心的 DAT。<button type="button" className="button secondary" onClick={search.reload}>重新读取</button></p> : null}
  </div>;
}

function useROMSetSearch({ gameId, coreId, entryFile, value, datSHA256 }: {
  gameId: string; coreId: string; entryFile: string; value?: string; datSHA256: string;
}) {
  const [text, setText] = useState(""), [query, setQuery] = useState("");
  const [offset, setOffset] = useState(0), [active, setActive] = useState(0);
  useEffect(() => {
    const timer = setTimeout(() => { setQuery(text); setOffset(0); setActive(0); }, 200);
    return () => clearTimeout(timer);
  }, [text]);
  const load = useCallback(async (signal?: AbortSignal) => result(await api.GET("/api/v1/admin/games/{gameId}/runtime-options/rom-sets", {
    signal, params: { path: { gameId }, query: { coreId, entryFile, romSet: value ?? "", q: query, offset, limit: pageSize } },
  })), [gameId, coreId, entryFile, value, query, offset]);
  const candidates = useResource(load);
  const data = candidates.data;
  const status = searchStatus(data, value, datSHA256, text !== query);
  return { ...status, text, setText, active, setActive, offset, setOffset, busy: candidates.loading || text !== query, reload: candidates.reload,
    failure: candidates.error || status.failure };

}
function searchStatus(data: Schema<"ROMSetOptions"> | null, value: string | undefined, datSHA256: string, pending: boolean) {
  const mismatch = !!data && data.datSHA256 !== datSHA256, selected = data?.selected;
  return { invalid: !!value && !!data && !selected, selected,
    entries: mismatch || pending ? [] : data?.entries ?? [], total: data?.total ?? 0,
    display: selected ? `${selected.description} · ${selected.name}` : value ?? "未匹配，请搜索并选择 ROM 集",
    failure: mismatch ? "DAT 与当前核心声明不一致，请重新加载。" : "" };
}

function ROMSetList({ id, entries, active, selected, busy, error, total, offset, onPage, onChoose }: {
  id: string; entries: Schema<"ROMSetEntry">[]; active: number; selected?: string; busy: boolean; error: string;
  total: number; offset: number; onPage: (offset: number) => void; onChoose: (name: string) => void;
}) {
  return <div className="rom-set-results">
    <div className="rom-set-options" id={id} role="listbox" aria-label="当前核心 DAT 条目">
      {entries.map((entry, index) => <button key={entry.name} id={`${id}-${index}`} type="button" role="option"
        aria-selected={selected === entry.name} className={active === index ? "is-active" : ""}
        onMouseDown={event => event.preventDefault()} onClick={() => onChoose(entry.name)}>
        <span>{entry.description}</span><small>{[entry.name, entry.year, entry.manufacturer].filter(Boolean).join(" · ")}</small>
      </button>)}
      {busy ? <p role="status">正在读取 DAT…</p> : error ? <p role="alert">{error}</p> : !entries.length ? <p>没有匹配的可运行游戏。</p> : null}
    </div>
    {total > pageSize ? <div className="list-pagination" aria-label="DAT 搜索分页">
      <span>{offset + 1}–{Math.min(total, offset + pageSize)} / {total}</span><div>
        <button type="button" disabled={busy || offset === 0} onClick={() => onPage(Math.max(0, offset - pageSize))}>上一页</button>
        <button type="button" disabled={busy || offset + pageSize >= total} onClick={() => onPage(offset + pageSize)}>下一页</button>
      </div>
    </div> : null}
  </div>;
}
