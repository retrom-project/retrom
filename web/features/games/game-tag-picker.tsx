"use client";
import { useHorizontalWheel } from "@/lib/use-horizontal-wheel";
import { useId, useState } from "react";
import type { Tag } from "@/lib/api/types";
import { AppIcon } from "@/components/app-icon";
export function GameTagPicker({ tags, selected, onChange }: {
  tags: Tag[]; selected: string[]; onChange: (ids: string[]) => void;
}) {
  const tagRail = useHorizontalWheel<HTMLDivElement>();
  const id = useId();
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const options = tags.filter((tag) => tag.name.toLocaleLowerCase().includes(query.toLocaleLowerCase()));
  function toggle(tagId: string) {
    onChange(selected.includes(tagId) ? selected.filter((value) => value !== tagId) : [...selected, tagId]);
  }
  return <div className="admin-game-tag-picker" onBlur={(event) => {
    if (!event.currentTarget.contains(event.relatedTarget)) { setOpen(false); }
  }}>
    <label className="field">标签
      <input value={query} placeholder="搜索并添加标签" role="combobox" aria-autocomplete="list" aria-expanded={open} aria-controls={id}
        aria-activedescendant={open && options[active] ? `${id}-${options[active].id}` : undefined}
        onFocus={() => setOpen(true)}
        onChange={(event) => { setQuery(event.target.value); setActive(0); setOpen(true); }}
        onKeyDown={(event) => {
          if (event.key === "Escape") { setOpen(false); }
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault(); setOpen(true);
            setActive((index) => Math.max(0, Math.min(options.length - 1, index + (event.key === "ArrowDown" ? 1 : -1))));
          }
          if (event.key === "Enter" && open) {
            event.preventDefault();
            if (options[active]) { toggle(options[active].id); }
          }
        }} />
    </label>
    {open ? <div className="admin-game-tag-list" role="listbox" aria-label="可用标签" aria-multiselectable="true" id={id}>
      {options.map((tag, index) => <button type="button" role="option" aria-selected={selected.includes(tag.id)} id={`${id}-${tag.id}`} className={active === index ? "is-active" : ""} key={tag.id} onMouseDown={(event) => event.preventDefault()} onClick={() => toggle(tag.id)}>
        <span>{tag.name}</span>{selected.includes(tag.id) ? <AppIcon name="check" /> : null}
      </button>)}
      {!options.length ? <p>没有匹配的标签，可先前往标签管理创建。</p> : null}
    </div> : null}
    <small>已选择 {selected.length} 个标签</small>
    <div ref={tagRail} className="admin-game-selected-tags">
      {tags.filter((tag) => selected.includes(tag.id)).map((tag) => <span className="tag-chip tag-chip-removable" key={tag.id}><span className="tag-chip-label">{tag.name}</span><button type="button" aria-label={`移除标签${tag.name}`} onClick={() => toggle(tag.id)}><AppIcon name="x" /></button></span>)}
      {!selected.length ? <span>未设置标签</span> : null}
    </div>
  </div>;
}
