"use client";
import { useMemo, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import type { GameQuery } from "./api";

export type ListFilters = {
  q?: string;
  platformInstanceId?: string;
  platformId?: string;
  tagId?: string;
  folderId?: string;
  offset?: string;
  sort?: string;
  scanId?: string;
};
type FilterValues = {
  q: string;
  directory: string;
  platform: string;
  tag: string;
  folder: string;
  sort: "title" | "recent";
  offset: number;
};

export function useLibraryQuery(initial: ListFilters) {
  const [values, setValues] = useState(() => initialValues(initial));
  const router = useRouter();
  const pathname = usePathname();
  const query = useMemo(() => gameQuery(values), [values]);
  function update(changes: Partial<FilterValues>, resetPage = true) {
    const next = { ...values, ...changes };
    if (resetPage) {
      next.offset = 0;
    }
    setValues(next);
    const params = new URLSearchParams();
    const address = {
      q: next.q,
      platformInstanceId: next.directory,
      platformId: next.platform,
      tagId: next.tag,
      folderId: next.folder,
      sort: next.sort,
      offset: next.offset ? String(next.offset) : "",
    };
    for (const [key, value] of Object.entries(address)) {
      if (value) {
        params.set(key, value);
      }
    }
    if (pathname === "/admin/reviews" && initial.scanId) {
      params.set("scanId", initial.scanId);
    }
    router.replace(`${pathname}?${params}`, { scroll: false });
  }
  return { values, query, update };
}

function initialValues(initial: ListFilters): FilterValues {
  return {
    q: initial.q ?? "",
    directory: initial.platformInstanceId ?? "",
    platform: initial.platformId ?? "",
    tag: initial.tagId ?? "",
    folder: initial.folderId ?? "",
    sort: initial.sort === "recent" ? "recent" : "title",
    offset: Math.max(0, Number(initial.offset) || 0),
  };
}

function gameQuery(values: FilterValues): GameQuery {
  return {
    q: values.q,
    platformInstanceId: values.directory || undefined,
    platformId: values.platform || undefined,
    tagId: values.tag || undefined,
    folderId:
      values.folder && values.folder !== "unclassified"
        ? values.folder
        : undefined,
    unclassified: values.folder === "unclassified" || undefined,
    offset: values.offset,
    limit: 24,
    sort: values.sort,
  };
}
