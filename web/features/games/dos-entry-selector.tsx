"use client";
import { useCallback, useEffect, useId, useRef, useState } from "react";
import type { KeyboardEvent } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";

const coreMenu = "使用核心启动菜单";
export function DOSEntrySelector({ gameId, entryFile, value, onSelect }: {
  gameId: string; entryFile: string; value: string; onSelect: (path: string) => void;
}) {
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const load = useCallback(async () => {
    if (!entryFile) { return { entries: [] }; }
    return result(await api.GET("/api/v1/admin/games/{gameId}/runtime-options/dos", {
      params: { path: { gameId }, query: { entryFile } },
    }));
  }, [gameId, entryFile]);
  const candidates = useResource(load);
  const invalid = !!value && !!candidates.data && !candidates.data.entries.includes(value);
  const options = ["", ...(candidates.data?.entries ?? [])].filter((path) =>
    (path || coreMenu).toLocaleLowerCase().includes(query.toLocaleLowerCase()));
  useEffect(() => { input.current?.setCustomValidity(invalid ? "原启动程序不在当前游戏包中，请重新选择。" : ""); }, [invalid]);
  useEffect(() => {
    if (open) { document.getElementById(`${id}-${active}`)?.scrollIntoView?.({ block: "nearest" }); }
  }, [open, active, id]);
  function choose(path: string) { onSelect(path); setOpen(false); setQuery(""); }
  function keydown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Escape") { event.preventDefault(); setOpen(false); setQuery(""); }
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault(); setOpen(true);
      setActive((index) => Math.max(0, Math.min(options.length - 1, index + (event.key === "ArrowDown" ? 1 : -1))));
    }
    if (event.key === "Enter" && open) {
      event.preventDefault();
      if (options[active] !== undefined) { choose(options[active]); }
    }
  }
  return <div className="dos-entry-selector" onBlur={(event) => {
    if (!event.currentTarget.contains(event.relatedTarget)) { setOpen(false); setQuery(""); }
  }}>
    <label className="field">启动程序
      <input ref={input} role="combobox" aria-autocomplete="list" aria-expanded={open} aria-controls={id}
        aria-activedescendant={open && options[active] !== undefined ? `${id}-${active}` : undefined}
        aria-invalid={invalid} aria-describedby={invalid ? `${id}-invalid` : undefined}
        title={value || coreMenu} value={open ? query : value || coreMenu} placeholder="搜索包内程序的完整路径"
        onFocus={() => { setQuery(""); setActive(0); setOpen(true); }}
        onClick={() => setOpen(true)}
        onChange={(event) => { setQuery(event.target.value); setActive(0); setOpen(true); }} onKeyDown={keydown} />
    </label>
    {open ? <DOSEntryOptions id={id} options={options} active={active} value={value} loading={candidates.loading} error={candidates.error} onChoose={choose} /> : null}
    {invalid ? <p role="alert" id={`${id}-invalid`}>原启动程序“{value}”在当前游戏包中不存在，请重新选择并保存。</p> : null}
    {candidates.error ? <p role="alert">无法读取包内启动程序。<button className="button secondary" type="button" onClick={candidates.reload}>重试读取</button></p> : null}
    <small>支持 .exe、.com、.bat；选择核心启动菜单时不指定程序。</small>
  </div>;
}

function DOSEntryOptions({ id, options, active, value, loading, error, onChoose }: {
  id: string; options: string[]; active: number; value: string; loading: boolean; error: string; onChoose: (path: string) => void;
}) {
  return <div className="dos-entry-options" id={id} role="listbox" aria-label="包内启动程序">
    {options.map((path, index) => <button key={path} id={`${id}-${index}`} type="button" role="option" aria-selected={value === path}
      className={active === index ? "is-active" : ""} onMouseDown={(event) => event.preventDefault()} onClick={() => onChoose(path)}>{path || coreMenu}</button>)}
    {loading ? <p role="status">正在读取包内程序…</p> : null}
    {!loading && !error && !options.length ? <p>没有匹配的启动程序。</p> : null}
  </div>;
}
