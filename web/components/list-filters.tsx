"use client";

import Link from "next/link";
import { useState } from "react";

export type FilterOption = { label: string; value: string; parentValue?: string };
export type FilterDefinition = { dependsOn?: string; label: string; name: string; options: FilterOption[] };
export type TextFilterDefinition = { label: string; name: string; placeholder: string };

export function ListFilters({ action, placeholder, values, filters = [], textFilters = [] }: { action: string; placeholder: string; values: Record<string, string>; filters?: FilterDefinition[]; textFilters?: TextFilterDefinition[] }) {
  const [selections, setSelections] = useState<Record<string, string>>(() => Object.fromEntries(filters.map((filter) => [filter.name, values[filter.name] ?? ""])));

  function select(name: string, value: string) {
    setSelections((current) => {
      const next = { ...current, [name]: value };
      for (const dependent of filters.filter((filter) => filter.dependsOn === name)) {
        const selected = dependent.options.find((option) => option.value === next[dependent.name]);
        if (selected?.parentValue && selected.parentValue !== value) next[dependent.name] = "";
      }
      return next;
    });
  }

  return <form className="filter-bar" action={action} method="get"><label className="search"><span aria-hidden="true">⌕</span><input aria-label="搜索" name="q" defaultValue={values.q} placeholder={placeholder} /></label>{textFilters.map((filter) => <label className="filter-text" key={filter.name}><span className="sr-only">{filter.label}</span><input aria-label={filter.label} name={filter.name} defaultValue={values[filter.name]} placeholder={filter.placeholder} /></label>)}{filters.map((filter) => {
    const parentValue = filter.dependsOn ? selections[filter.dependsOn] : "";
    const options = filter.options.filter((option) => !option.parentValue || !parentValue || option.parentValue === parentValue);
    return <label className="filter-field" key={filter.name}><span className="sr-only">{filter.label}</span><select className="select" aria-label={filter.label} name={filter.name} value={selections[filter.name]} onChange={(event) => select(filter.name, event.target.value)}>{options.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}</select></label>;
  })}<button className="button secondary" type="submit">应用筛选</button><Link className="row-action filter-reset" href={action}>清除</Link></form>;
}
