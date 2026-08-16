"use client";

import Link from "next/link";
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";

export type TagReference = { tagId: string; name: string };

type ListPosition = {
  above: boolean;
  left: number;
  maxHeight: number;
  top: number;
  width: number;
};

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
  const [listPosition, setListPosition] = useState<ListPosition | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const selectedIDs = useMemo(() => new Set(selected.map((tag) => tag.tagId)), [selected]);
  const filtered = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase();
    return [...options].filter((tag) => !selectedIDs.has(tag.tagId) && (!normalized || tag.name.toLocaleLowerCase().includes(normalized)))
      .sort((left, right) => left.name.localeCompare(right.name, "zh-CN"));
  }, [options, query, selectedIDs]);
  const atLimit = selected.length >= 20;

  const updateListPosition = useCallback(() => {
    const input = inputRef.current;
    if (!input) return;
    const rect = input.getBoundingClientRect();
    const viewportWidth = document.documentElement.clientWidth || window.innerWidth;
    const viewportHeight = document.documentElement.clientHeight || window.innerHeight;
    const margin = 8;
    const gap = 5;
    const preferredHeight = 240;
    const below = viewportHeight - rect.bottom - gap - margin;
    const above = rect.top - gap - margin;
    const placeAbove = below < Math.min(160, preferredHeight) && above > below;
    const availableHeight = placeAbove ? above : below;
    const width = Math.min(rect.width, Math.max(0, viewportWidth - margin * 2));
    const left = Math.min(Math.max(rect.left, margin), Math.max(margin, viewportWidth - width - margin));
    setListPosition({
      above: placeAbove,
      left,
      maxHeight: Math.max(72, Math.min(preferredHeight, availableHeight)),
      top: placeAbove ? rect.top - gap : rect.bottom + gap,
      width,
    });
  }, []);

  useEffect(() => {
    if (!open) return;
    const input = inputRef.current;
    const observer = input && typeof ResizeObserver !== "undefined" ? new ResizeObserver(updateListPosition) : null;
    if (input) observer?.observe(input);
    window.addEventListener("resize", updateListPosition);
    window.addEventListener("scroll", updateListPosition, true);
    window.visualViewport?.addEventListener("resize", updateListPosition);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", updateListPosition);
      window.removeEventListener("scroll", updateListPosition, true);
      window.visualViewport?.removeEventListener("resize", updateListPosition);
    };
  }, [open, updateListPosition]);

  function openList() {
    updateListPosition();
    setOpen(true);
  }

  function closeList() {
    setOpen(false);
    setListPosition(null);
  }

  function choose(tag: TagReference) {
    if (atLimit) return;
    onChange([...selected, tag].sort((left, right) => left.name.localeCompare(right.name, "zh-CN")));
    setQuery("");
    setActiveIndex(0);
    closeList();
    inputRef.current?.focus();
  }

  const list = open && !disabled && !atLimit && listPosition ? <div
    className={`tag-picker-list tag-picker-list-floating${listPosition.above ? " is-above" : ""}`}
    id={listId}
    role="listbox"
    style={{ left: listPosition.left, maxHeight: listPosition.maxHeight, top: listPosition.top, width: listPosition.width }}
  >{filtered.length ? filtered.map((tag, index) => <button
    aria-selected={index === activeIndex}
    className={index === activeIndex ? "is-active" : ""}
    id={`${listId}-${tag.tagId}`}
    key={tag.tagId}
    onMouseDown={(event) => event.preventDefault()}
    onMouseEnter={() => setActiveIndex(index)}
    onClick={() => choose(tag)}
    role="option"
    type="button"
  >{tag.name}</button>) : <div className="tag-picker-empty">{options.length ? "没有匹配的活动标签" : <>还没有活动标签。<Link href="/admin/tags">前往标签管理</Link></>}</div>}</div> : null;

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
        onBlur={() => window.setTimeout(closeList, 120)}
        onFocus={openList}
        onChange={(event) => { setQuery(event.target.value); setActiveIndex(0); openList(); }}
        onKeyDown={(event) => {
          if (event.key === "Escape") { event.stopPropagation(); closeList(); return; }
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault(); openList();
            const offset = event.key === "ArrowDown" ? 1 : -1;
            setActiveIndex((current) => filtered.length ? (current + offset + filtered.length) % filtered.length : 0);
          }
          if (event.key === "Enter" && open && filtered[activeIndex]) { event.preventDefault(); choose(filtered[activeIndex]); }
        }}
      />
    </div>
    {list && typeof document !== "undefined" ? createPortal(list, document.body) : null}
    <span className="tag-picker-limit" aria-live="polite">已选择 {selected.length}/20 个标签{atLimit ? "，已达到上限" : ""}</span>
  </div>;
}
