"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  type FormEvent,
  type KeyboardEvent,
  type RefObject,
  useEffect,
  useRef,
  useState,
  useTransition,
} from "react";
import { AppIcon } from "@/components/app-icon";

export type FilterOption = { label: string; value: string; parentValue?: string };
export type FilterDefinition = { dependsOn?: string; label: string; name: string; options: FilterOption[] };
export type TextFilterDefinition = { label: string; name: string; placeholder: string };
export type FixedFilterDefinition = { name: string; value: string };

const focusableSelector = "button:not(:disabled), input:not(:disabled), select:not(:disabled), a[href], [tabindex]:not([tabindex='-1'])";

type ListFiltersProps = {
  action: string;
  placeholder: string;
  values: Record<string, string>;
  filters?: FilterDefinition[];
  textFilters?: TextFilterDefinition[];
  fixedFilters?: FixedFilterDefinition[];
  preserveFixedFiltersOnReset?: boolean;
  resultCount?: number;
};

export function ListFilters(input: ListFiltersProps) {
  return <ListFiltersContent
    action={input.action}
    placeholder={input.placeholder}
    values={input.values}
    filters={input.filters ?? []}
    textFilters={input.textFilters ?? []}
    fixedFilters={input.fixedFilters ?? []}
    preserveFixedFiltersOnReset={input.preserveFixedFiltersOnReset ?? false}
    resultCount={input.resultCount}
  />;
}

function ListFiltersContent({ action, placeholder, values, filters, textFilters, fixedFilters, preserveFixedFiltersOnReset, resultCount }: Required<Omit<ListFiltersProps, "resultCount">> & Pick<ListFiltersProps, "resultCount">) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const filterNames = ["q", ...textFilters.map((filter) => filter.name), ...filters.map((filter) => filter.name)];
  const selectionKey = JSON.stringify(filterNames.map((name) => [name, values[name] ?? ""]));
  const [selectionState, setSelectionState] = useState(() => ({
    sourceKey: selectionKey,
    values: Object.fromEntries(filterNames.map((name) => [name, values[name] ?? ""])),
  }));
  const [filterOpen, setFilterOpen] = useState(false);
  const filterTriggerRef = useRef<HTMLButtonElement>(null);
  const filterPanelRef = useRef<HTMLDivElement>(null);
  const draftSnapshotRef = useRef<Record<string, string> | null>(null);
  if (selectionState.sourceKey !== selectionKey) {
    setSelectionState({
      sourceKey: selectionKey,
      values: Object.fromEntries(filterNames.map((name) => [name, values[name] ?? ""])),
    });
  }
  const selections = selectionState.sourceKey === selectionKey
    ? selectionState.values
    : Object.fromEntries(filterNames.map((name) => [name, values[name] ?? ""]));

  useEffect(() => {
    if (!filterOpen) {return;}
    const previousOverflow = document.body.style.overflow;
    const previousPaddingRight = document.body.style.paddingRight;
    const returnFocus = filterTriggerRef.current;
    const scrollbarWidth = window.innerWidth - document.documentElement.clientWidth;
    document.body.style.overflow = "hidden";
    if (scrollbarWidth > 0) {document.body.style.paddingRight = `${scrollbarWidth}px`;}
    const frame = requestAnimationFrame(() => filterPanelRef.current?.querySelector<HTMLElement>(focusableSelector)?.focus());
    return () => {
      cancelAnimationFrame(frame);
      document.body.style.overflow = previousOverflow;
      document.body.style.paddingRight = previousPaddingRight;
      returnFocus?.focus();
    };
  }, [filterOpen]);

  function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const query = new URLSearchParams();
    for (const [name, raw] of new FormData(event.currentTarget)) {
      const value = String(raw).trim();
      if (value) {query.set(name, value);}
    }
    const encoded = query.toString();
    setFilterOpen(false);
    startTransition(() => router.replace(encoded ? `${action}?${encoded}` : action, { scroll: false }));
  }

  function update(name: string, value: string) {
    setSelectionState((current) => {
      const next = { ...current.values, [name]: value };
      for (const dependent of filters.filter((filter) => filter.dependsOn === name)) {
        const selected = dependent.options.find((option) => option.value === next[dependent.name]);
        if (selected?.parentValue && selected.parentValue !== value) {next[dependent.name] = "";}
      }
      return { sourceKey: selectionKey, values: next };
    });
  }

  function openFilters() {
    draftSnapshotRef.current = { ...selections };
    setFilterOpen(true);
  }

  function cancelFilters() {
    const snapshot = draftSnapshotRef.current;
    if (snapshot) {setSelectionState({ sourceKey: selectionKey, values: snapshot });}
    setFilterOpen(false);
  }

  function trapFilterFocus(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      cancelFilters();
      return;
    }
    if (event.key !== "Tab") {return;}
    const focusable = Array.from(filterPanelRef.current?.querySelectorAll<HTMLElement>(focusableSelector) ?? []);
    const first = focusable[0];
    const last = focusable.at(-1);
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last?.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first?.focus();
    }
  }

  const { hasActiveFilters, resetHref } = filterSummaryState(
    action, values, fixedFilters, preserveFixedFiltersOnReset,
  );
  const hasAdvancedFilters = textFilters.length > 0 || filters.length > 0;
  const advancedCount = [...textFilters.map((filter) => filter.name), ...filters.map((filter) => filter.name)]
    .filter((name) => Boolean(values[name])).length;

  return <form className="filter-bar" action={action} method="get" onSubmit={search} aria-busy={pending}>
    {fixedFilters.map((filter) => <input key={filter.name} type="hidden" name={filter.name} value={filter.value} />)}
    <div className="filter-controls">
      <div className="filter-primary-row">
        <label className="filter-control filter-search"><span className="filter-label">搜索</span><span className="search"><AppIcon name="search" /><input aria-label="搜索" name="q" value={selections.q} onChange={(event) => update("q", event.target.value)} placeholder={placeholder} /></span></label>
        {hasAdvancedFilters ? <button ref={filterTriggerRef} className="button secondary filter-mobile-trigger" type="button" aria-expanded={filterOpen} aria-controls="responsive-filter-sheet" onClick={openFilters}><AppIcon name="settings" /><span>筛选与排序{advancedCount ? `（${advancedCount}）` : ""}</span></button> : <button className="button filter-submit filter-search-submit" type="submit" disabled={pending}>{pending ? "搜索中…" : "搜索"}</button>}
      </div>
      <AdvancedFilterSheet {...{
        cancelFilters, filterOpen, filterPanelRef, filters, pending, selections, textFilters, trapFilterFocus, update,
      }} visible={hasAdvancedFilters} />
    </div>
    <div className="filter-summary">
      <span className="filter-hint">{hasActiveFilters ? "筛选条件已应用" : "未设置筛选条件"}</span>
      <div className="filter-result">{resultCount === undefined ? null : <span>当前显示 <strong>{resultCount}</strong> 项</span>}{hasActiveFilters ? <Link className="row-action filter-reset" href={resetHref}>清除全部</Link> : null}</div>
    </div>
  </form>;
}

function AdvancedFilterSheet({
  cancelFilters, filterOpen, filterPanelRef, filters, pending, selections, textFilters, trapFilterFocus, update,
  visible,
}: {
  cancelFilters: () => void;
  filterOpen: boolean;
  filterPanelRef: RefObject<HTMLDivElement | null>;
  filters: FilterDefinition[];
  pending: boolean;
  selections: Record<string, string>;
  textFilters: TextFilterDefinition[];
  trapFilterFocus: (event: KeyboardEvent<HTMLDivElement>) => void;
  update: (name: string, value: string) => void;
  visible: boolean;
}) {
  if (!visible) {return null;}
  const clearFilters = () => {
    for (const filter of textFilters) {update(filter.name, "");}
    for (const filter of filters) {update(filter.name, "");}
  };
  return <>
    <button className={`filter-sheet-backdrop${filterOpen ? " is-open" : ""}`} type="button" tabIndex={-1} aria-label="关闭筛选与排序" onClick={cancelFilters} />
    <div
      ref={filterPanelRef}
      id="responsive-filter-sheet"
      className={`filter-sheet${filterOpen ? " is-open" : ""}`}
      role={filterOpen ? "dialog" : undefined}
      aria-modal={filterOpen ? "true" : undefined}
      aria-labelledby={filterOpen ? "responsive-filter-title" : undefined}
      onKeyDown={trapFilterFocus}
    >
      <header className="filter-sheet-head"><div><h2 id="responsive-filter-title">筛选与排序</h2><p>调整条件后一次应用到当前列表。</p></div><button type="button" aria-label="关闭筛选与排序" onClick={cancelFilters}><AppIcon name="x" /></button></header>
      <div className="filter-sheet-body">
        {textFilters.map((filter) => <label className="filter-control filter-text" key={filter.name}><span className="filter-label">{filter.label}</span><input aria-label={filter.label} name={filter.name} value={selections[filter.name]} onChange={(event) => update(filter.name, event.target.value)} placeholder={filter.placeholder} /></label>)}
        {filters.map((filter) => {
          const parentValue = filter.dependsOn ? selections[filter.dependsOn] : "";
          const options = filter.options.filter((option) => !option.parentValue || !parentValue || option.parentValue === parentValue);
          return <label className="filter-control filter-field" key={filter.name}><span className="filter-label">{filter.label}</span><select className="select" aria-label={filter.label} name={filter.name} value={selections[filter.name]} onChange={(event) => update(filter.name, event.target.value)}>{options.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}</select></label>;
        })}
      </div>
      <footer className="filter-sheet-footer">
        <button className="button secondary filter-sheet-clear" type="button" onClick={clearFilters}>清除筛选</button>
        <button className="button secondary filter-sheet-cancel" type="button" onClick={cancelFilters}>取消</button>
        <button className="button filter-submit" type="submit" disabled={pending}>{pending ? "应用中…" : "应用筛选"}</button>
      </footer>
    </div>
  </>;
}

function filterSummaryState(
  action: string,
  values: Record<string, string>,
  fixedFilters: FixedFilterDefinition[],
  preserveFixedFiltersOnReset: boolean,
) {
  const fixedFilterNames = new Set(fixedFilters.map((filter) => filter.name));
  const hasActiveFilters = Object.entries(values)
    .some(([name, value]) => !fixedFilterNames.has(name) && Boolean(value));
  const fixedParameters = new URLSearchParams();
  for (const filter of fixedFilters) {fixedParameters.set(filter.name, filter.value);}
  const fixedQuery = fixedParameters.toString();
  const resetHref = preserveFixedFiltersOnReset && fixedQuery ? `${action}?${fixedQuery}` : action;
  return { hasActiveFilters, resetHref };
}
