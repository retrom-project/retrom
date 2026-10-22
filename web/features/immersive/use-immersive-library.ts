"use client";
import { useSearchParams, type ReadonlyURLSearchParams } from "next/navigation";
import { useCallback, useState } from "react";
import { useResource } from "@/lib/use-resource";
import type { PageRequest } from "@/lib/use-infinite-resource";
import { entryId, loadDestinations, loadEntries, loadFolders, loadLetters, type ImmersiveEntry, type View } from "./immersive-data";
import { useEntryWindow } from "./use-entry-window";
import { useSaveSelection, type RestoredSaveSelection } from "./use-save-selection";
export function useImmersiveLibrary() {
  const search = useSearchParams();
  const destinations = useResource(loadDestinations);
  const [view, setView] = useState<View>(() => restoredView(search.get("view")));
  const [position, setPosition] = useState(0);
  const [selectionId, setSelectionId] = useState(search.get("entry") ?? "");
  const [destinationId, setDestinationId] = useState(search.get("destination") ?? "all");
  const destination = Math.max(0, destinations.data?.items.findIndex((item) => item.id === destinationId) ?? 0);
  const [folder, setFolder] = useState(search.get("folder") ?? "");
  const folders = useResource(loadFolders);
  const current = destinations.data?.items[destination];
  const directoryId = view === "games" && destinationId !== "all" ? destinationId : undefined;
  const restoreOffset = restoredEntryOffset(search, view, destinationId, folder);
  const entries = useEntryWindow(useCallback(({ offset, limit, signal }: PageRequest) => loadEntries(view, directoryId, folder, offset, limit, signal), [view, directoryId, folder]), restoreOffset);
  const letters = useResource(useCallback((signal?: AbortSignal) => loadLetters(view, directoryId, folder, signal), [view, directoryId, folder]));
  // The selection is a list position, so pages loaded before the window do not move it to another game.
  const selected = selectedEntryIndex(entries.data?.items, selectionId, position - entries.base);
  function setSelected(next: number | ((previous: number) => number)) { setSelectionId(""); setPosition(entries.base + (typeof next === "function" ? next(selected) : next)); }
  function setListPosition(next: number) { setSelectionId(""); setPosition(Math.max(0, next)); }
  function setDestination(next: number | ((previous: number) => number)) { const index = typeof next === "function" ? next(destination) : next; setDestinationId(destinations.data?.items[index]?.id ?? "all"); }
  const selectedEntry = entries.data?.items[selected];
  const saves = useSaveSelection(selectedEntry, view === "saves", restoredSaveSelection(search));
  const query = new URLSearchParams({ view, destination: destinationId, offset: String(Math.floor((entries.base + selected) / 24) * 24), folder, entry: entryId(selectedEntry) });
  if (saves.save) { query.set("save", saves.save.id); query.set("saveOffset", String(saves.resource.data?.offset ?? 0)); }
  const returnTo = `/immersive?${query}`;
  return { destinations, view, setView, selected, setSelected, setListPosition, destination, setDestination, current, entries, letters, folder, setFolder, folders, returnTo, saves };
}
function restoredView(value: string | null): View { return value === "games" || value === "favorites" || value === "saves" || value === "recent" ? value : "platforms"; }

function selectedEntryIndex(items: ImmersiveEntry[] = [], selectionId: string, index: number) {
  const restored = items.findIndex((entry) => entryId(entry) === selectionId);
  return restored >= 0 ? restored : Math.max(0, Math.min(index, items.length - 1));
}

/** Returning from play reopens the list at the page of the selected game instead of its first page. */
function restoredEntryOffset(search: ReadonlyURLSearchParams, view: View, destinationId: string, folder: string) {
  if (view !== restoredView(search.get("view")) || destinationId !== (search.get("destination") ?? "all") || folder !== (search.get("folder") ?? "")) { return 0; }
  return Math.max(0, Number(search.get("offset")) || 0);
}

function restoredSaveSelection(search: ReadonlyURLSearchParams): RestoredSaveSelection {
  return { entryId: search.get("entry") ?? "", saveId: search.get("save") ?? "", offset: search.has("saveOffset") ? Math.max(0, Number(search.get("saveOffset")) || 0) : undefined };
}
