"use client";
import { useSearchParams, type ReadonlyURLSearchParams } from "next/navigation";
import { useCallback, useState } from "react";
import { useResource } from "@/lib/use-resource";
import { entryId, loadDestinations, loadEntries, loadFolders, type View } from "./immersive-data";
import { useSaveSelection, type RestoredSaveSelection } from "./use-save-selection";
export function useImmersiveLibrary() {
  const search = useSearchParams();
  const destinations = useResource(loadDestinations);
  const [view, setView] = useState<View>(() => restoredView(search.get("view")));
  const [position, setPosition] = useState(0);
  const [selectionId, setSelectionId] = useState(search.get("entry") ?? "");
  const [destinationId, setDestinationId] = useState(search.get("destination") ?? "all");
  const destination = Math.max(0, destinations.data?.items.findIndex((item) => item.id === destinationId) ?? 0);
  const [offset, setOffset] = useState(() => Math.max(0, Number(search.get("offset")) || 0));
  const [folder, setFolder] = useState(search.get("folder") ?? "");
  const folders = useResource(loadFolders);
  const current = destinations.data?.items[destination];
  const entries = useResource(useCallback(() => loadEntries(view, current?.directoryId, folder, offset), [view, current?.directoryId, folder, offset]));
  const restoredIndex = entries.data?.items.findIndex((entry) => entryId(entry) === selectionId) ?? -1;
  const selected = restoredIndex >= 0 ? restoredIndex : position;
  function setSelected(next: number | ((previous: number) => number)) { setSelectionId(""); setPosition(typeof next === "function" ? next(selected) : next); }
  function setDestination(next: number | ((previous: number) => number)) { const index = typeof next === "function" ? next(destination) : next; setDestinationId(destinations.data?.items[index]?.id ?? "all"); }
  const selectedEntry = entries.data?.items[selected];
  const saves = useSaveSelection(selectedEntry, view === "saves", restoredSaveSelection(search));
  const query = new URLSearchParams({ view, destination: destinationId, offset: String(offset), folder, entry: entryId(selectedEntry) });
  if (saves.save) { query.set("save", saves.save.id); query.set("saveOffset", String(saves.resource.data?.offset ?? 0)); }
  const returnTo = `/immersive?${query}`;
  return { destinations, view, setView, selected, setSelected, destination, setDestination, current, entries, offset, setOffset, folder, setFolder, folders, returnTo, saves };
}
function restoredView(value: string | null): View { return value === "games" || value === "favorites" || value === "saves" || value === "recent" ? value : "platforms"; }

function restoredSaveSelection(search: ReadonlyURLSearchParams): RestoredSaveSelection {
  return { entryId: search.get("entry") ?? "", saveId: search.get("save") ?? "", offset: search.has("saveOffset") ? Math.max(0, Number(search.get("saveOffset")) || 0) : undefined };
}
