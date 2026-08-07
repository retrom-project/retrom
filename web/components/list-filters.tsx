"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { type FormEvent, useState, useTransition } from "react";
import { AppIcon } from "@/components/app-icon";

export type FilterOption = { label: string; value: string; parentValue?: string };
export type FilterDefinition = { dependsOn?: string; label: string; name: string; options: FilterOption[] };
export type TextFilterDefinition = { label: string; name: string; placeholder: string };
export type FixedFilterDefinition = { label: string; name: string; value: string };

function hrefWithout(action: string, values: Record<string, string>, name: string) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) if (key !== name && value) query.set(key, value);
  const encoded = query.toString();
  return encoded ? `${action}?${encoded}` : action;
}

export function ListFilters({ action, placeholder, values, filters = [], textFilters = [], fixedFilters = [], resultCount }: { action: string; placeholder: string; values: Record<string, string>; filters?: FilterDefinition[]; textFilters?: TextFilterDefinition[]; fixedFilters?: FixedFilterDefinition[]; resultCount?: number }) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const selectionKey = JSON.stringify(filters.map((filter) => [filter.name, values[filter.name] ?? ""]));
  const [selectionState, setSelectionState] = useState(() => ({
    sourceKey: selectionKey,
    values: Object.fromEntries(filters.map((filter) => [filter.name, values[filter.name] ?? ""])),
  }));
  if (selectionState.sourceKey !== selectionKey) {
    setSelectionState({
      sourceKey: selectionKey,
      values: Object.fromEntries(filters.map((filter) => [filter.name, values[filter.name] ?? ""])),
    });
  }
  const selections = selectionState.sourceKey === selectionKey
    ? selectionState.values
    : Object.fromEntries(filters.map((filter) => [filter.name, values[filter.name] ?? ""]));

  function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const query = new URLSearchParams();
    for (const [name, raw] of new FormData(event.currentTarget)) {
      const value = String(raw).trim();
      if (value) query.set(name, value);
    }
    const encoded = query.toString();
    startTransition(() => router.replace(encoded ? `${action}?${encoded}` : action, { scroll: false }));
  }

  function select(name: string, value: string) {
    setSelectionState((current) => {
      const next = { ...current.values, [name]: value };
      for (const dependent of filters.filter((filter) => filter.dependsOn === name)) {
        const selected = dependent.options.find((option) => option.value === next[dependent.name]);
        if (selected?.parentValue && selected.parentValue !== value) next[dependent.name] = "";
      }
      return { sourceKey: selectionKey, values: next };
    });
  }

  const hasActiveFilters = Object.values(values).some((value) => Boolean(value));
  const chips = Object.entries(values).flatMap(([name, value]) => {
    if (!value) return [];
    if (name === "q") return [{ name, label: `关键词：${value}` }];
    const fixedFilter = fixedFilters.find((filter) => filter.name === name && filter.value === value);
    if (fixedFilter) return [{ name, label: fixedFilter.label }];
    const textFilter = textFilters.find((filter) => filter.name === name);
    if (textFilter) return [{ name, label: `${textFilter.label}：${value}` }];
    const filter = filters.find((item) => item.name === name);
    const option = filter?.options.find((item) => item.value === value);
    return filter ? [{ name, label: `${filter.label}：${option?.label ?? value}` }] : [];
  });

  return <form className="filter-bar" action={action} method="get" onSubmit={search} aria-busy={pending}>
    {fixedFilters.map((filter) => <input key={filter.name} type="hidden" name={filter.name} value={filter.value} />)}
    <div className="filter-controls">
      <label className="filter-control filter-search"><span className="filter-label">搜索</span><span className="search"><AppIcon name="search" /><input key={`q:${values.q ?? ""}`} aria-label="搜索" name="q" defaultValue={values.q} placeholder={placeholder} /></span></label>
      {textFilters.map((filter) => <label className="filter-control filter-text" key={filter.name}><span className="filter-label">{filter.label}</span><input key={`${filter.name}:${values[filter.name] ?? ""}`} aria-label={filter.label} name={filter.name} defaultValue={values[filter.name]} placeholder={filter.placeholder} /></label>)}
      {filters.map((filter) => {
        const parentValue = filter.dependsOn ? selections[filter.dependsOn] : "";
        const options = filter.options.filter((option) => !option.parentValue || !parentValue || option.parentValue === parentValue);
        return <label className="filter-control filter-field" key={filter.name}><span className="filter-label">{filter.label}</span><select className="select" aria-label={filter.label} name={filter.name} value={selections[filter.name]} onChange={(event) => select(filter.name, event.target.value)}>{options.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}</select></label>;
      })}
      <button className="button filter-submit" type="submit" disabled={pending}>{pending ? "搜索中…" : "搜索"}</button>
    </div>
    <div className="filter-summary">
      <div className="filter-chips" aria-label="当前筛选条件">{chips.length ? chips.map((chip) => <Link className="filter-chip" href={hrefWithout(action, values, chip.name)} aria-label={`移除${chip.label}`} key={chip.name}>{chip.label}<span aria-hidden="true">×</span></Link>) : <span className="filter-hint">未设置筛选条件</span>}</div>
      <div className="filter-result">{resultCount === undefined ? null : <span>当前显示 <strong>{resultCount}</strong> 项</span>}{hasActiveFilters ? <Link className="row-action filter-reset" href={action}>清除全部</Link> : null}</div>
    </div>
  </form>;
}
