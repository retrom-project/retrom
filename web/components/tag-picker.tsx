"use client";

import Link from "next/link";
import { useId, useMemo, useRef, useState } from "react";

export type TagReference = { tagId: string; name: string };

export function TagChips({ tags, limit, linked = false, label = "游戏标签", ariaLabel }: { tags: TagReference[]; limit?: number; linked?: boolean; label?: string; ariaLabel?: string }) {
  if (tags.length === 0) return null;
  const visible = limit === undefined ? tags : tags.slice(0, limit);
  const remaining = tags.length - visible.length;
  return <div className="tag-chips" aria-label={ariaLabel ?? label}>{visible.map((tag) => linked
    ? <Link className="tag-chip" href={`/library?tagId=${encodeURIComponent(tag.tagId)}`} aria-label={`查看标签“${tag.name}”下的游戏`} title={tag.name} key={tag.tagId}>{tag.name}</Link>
    : <span className="tag-chip" title={tag.name} key={tag.tagId}>{tag.name}</span>)}{remaining > 0 ? <span className="tag-chip tag-chip-more" aria-label={`另有 ${remaining} 个标签`}>+{remaining}</span> : null}</div>;
}

export function TagPicker({ label = "标签", options, selected, onChange, disabled = false, description }: {
  label?: string;
  options: TagReference[];
  selected: TagReference[];
  onChange: (tags: TagReference[]) => void;
  disabled?: boolean;
  description?: string;
}) {
  const inputId = useId();
  const listId = useId();
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const selectedIDs = useMemo(() => new Set(selected.map((tag) => tag.tagId)), [selected]);
  const filtered = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase();
    return [...options].filter((tag) => !selectedIDs.has(tag.tagId) && (!normalized || tag.name.toLocaleLowerCase().includes(normalized)))
      .sort((left, right) => left.name.localeCompare(right.name, "zh-CN"));
  }, [options, query, selectedIDs]);
  const atLimit = selected.length >= 20;

  function choose(tag: TagReference) {
    if (atLimit) return;
    onChange([...selected, tag].sort((left, right) => left.name.localeCompare(right.name, "zh-CN")));
    setQuery("");
    setActiveIndex(0);
    setOpen(false);
    inputRef.current?.focus();
  }

  return <div className="tag-picker">
    <label htmlFor={inputId}>{label}</label>
    {description ? <p className="field-help" id={`${inputId}-help`}>{description}</p> : null}
    <div className="tag-picker-selected" aria-label="已选择标签">{selected.map((tag) => <span className="tag-chip tag-chip-removable" key={tag.tagId} title={tag.name}>{tag.name}<button type="button" disabled={disabled} aria-label={`移除标签“${tag.name}”`} onClick={() => { onChange(selected.filter((entry) => entry.tagId !== tag.tagId)); window.requestAnimationFrame(() => inputRef.current?.focus()); }}>×</button></span>)}</div>
    <div className="tag-picker-combobox">
      <input
        id={inputId}
        ref={inputRef}
        role="combobox"
        aria-autocomplete="list"
        aria-controls={listId}
        aria-expanded={open}
        aria-describedby={description ? `${inputId}-help` : undefined}
        aria-activedescendant={open && filtered[activeIndex] ? `${listId}-${filtered[activeIndex].tagId}` : undefined}
        autoComplete="off"
        disabled={disabled || atLimit}
        placeholder={atLimit ? "已达到 20 个标签上限" : "搜索并添加标签"}
        value={query}
        onBlur={() => window.setTimeout(() => setOpen(false), 120)}
        onFocus={() => setOpen(true)}
        onChange={(event) => { setQuery(event.target.value); setActiveIndex(0); setOpen(true); }}
        onKeyDown={(event) => {
          if (event.key === "Escape") { setOpen(false); return; }
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault(); setOpen(true);
            const offset = event.key === "ArrowDown" ? 1 : -1;
            setActiveIndex((current) => filtered.length ? (current + offset + filtered.length) % filtered.length : 0);
          }
          if (event.key === "Enter" && open && filtered[activeIndex]) { event.preventDefault(); choose(filtered[activeIndex]); }
        }}
      />
      {open && !disabled && !atLimit ? <div className="tag-picker-list" id={listId} role="listbox">{filtered.length ? filtered.map((tag, index) => <button
        aria-selected={index === activeIndex}
        className={index === activeIndex ? "is-active" : ""}
        id={`${listId}-${tag.tagId}`}
        key={tag.tagId}
        onMouseDown={(event) => event.preventDefault()}
        onMouseEnter={() => setActiveIndex(index)}
        onClick={() => choose(tag)}
        role="option"
        type="button"
      >{tag.name}</button>) : <div className="tag-picker-empty">{options.length ? "没有匹配的活动标签" : <>还没有活动标签。<Link href="/admin/tags">前往标签管理</Link></>}</div>}</div> : null}
    </div>
    <span className="tag-picker-limit" aria-live="polite">已选择 {selected.length}/20 个标签{atLimit ? "，已达到上限" : ""}</span>
  </div>;
}
