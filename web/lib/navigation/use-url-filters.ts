"use client";

import { useCallback, useState, type SetStateAction } from "react";
import { useSearchParams } from "next/navigation";

// Writes originate in user actions. Navigation only restores state, so a cached
// page can never overwrite a Back/Forward URL with its previous filters.
export function useURLFilters<T>(parse: (search: URLSearchParams) => T, serialize: (value: T) => string) {
  const search = useSearchParams().toString();
  const [state, setState] = useState(() => ({value: parse(new URLSearchParams(search)), observed: search, written: search, navigation: 0}));
  const external = state.observed !== search && state.written !== search;
  const value = external ? parse(new URLSearchParams(search)) : state.value;
  const navigation = state.navigation + Number(external);
  if (state.observed !== search) {
    setState({value, observed: search, written: search, navigation});
  }
  const update = useCallback((change: SetStateAction<T>) => {
    const actual = window.location.search.slice(1);
    const current = actual === state.written ? state.value : parse(new URLSearchParams(actual));
    const next = typeof change === "function" ? (change as (previous: T) => T)(current) : change;
    const written = serialize(next);
    setState({value: next, observed: search, written, navigation: state.navigation});
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${written ? `?${written}` : ""}${window.location.hash}`);
  }, [parse, search, serialize, state, setState]);
  return [value, update, navigation] as const;
}
