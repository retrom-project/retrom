"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { type FormEvent, useState, useTransition } from "react";
import { AppIcon } from "@/components/app-icon";

export type FilterOption = { label: string; value: string; parentValue?: string };
export type FilterDefinition = { dependsOn?: string; label: string; name: string; options: FilterOption[] };
export type TextFilterDefinition = { label: string; name: string; placeholder: string };
export type FixedFilterDefinition = { name: string; value: string };

export function ListFilters({ action, placeholder, values, filters = [], textFilters = [], fixedFilters = [], preserveFixedFiltersOnReset = false, resultCount }: { action: string; placeholder: string; values: Record<string, string>; filters?: FilterDefinition[]; textFilters?: TextFilterDefinition[]; fixedFilters?: FixedFilterDefinition[]; preserveFixedFiltersOnReset?: boolean; resultCount?: number }) {
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

  const fixedFilterNames = new Set(fixedFilters.map((filter) => filter.name));
  const hasActiveFilters = Object.entries(values).some(([name, value]) => !fixedFilterNames.has(name) && Boolean(value));
  const fixedParameters = new URLSearchParams();
  for (const filter of fixedFilters) fixedParameters.set(filter.name, filter.value);
  const fixedQuery = fixedParameters.toString();
  const resetHref = preserveFixedFiltersOnReset && fixedQuery ? `${action}?${fixedQuery}` : action;

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
      <span className="filter-hint">{hasActiveFilters ? "筛选条件已应用" : "未设置筛选条件"}</span>
      <div className="filter-result">{resultCount === undefined ? null : <span>当前显示 <strong>{resultCount}</strong> 项</span>}{hasActiveFilters ? <Link className="row-action filter-reset" href={resetHref}>清除全部</Link> : null}</div>
    </div>
  </form>;
}
