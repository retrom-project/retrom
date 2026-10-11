"use client";
import { useCallback, useState } from "react";
import { api, result } from "@/lib/api/client";
import type { Save } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { entryId as getEntryId, type ImmersiveEntry } from "./immersive-data";

const PAGE_SIZE = 100;
type SavePage = { items: Save[]; offset: number; total: number };
type Selection = { entryId: string; offset?: number; saveId?: string; edge?: "first" | "last" };
export type RestoredSaveSelection = { entryId: string; saveId: string; offset?: number };
type SaveRequest = { entryId: string; anchorId: string; offset?: number; saveId?: string; edge?: "first" | "last" };

function saveRequest(entry: ImmersiveEntry | undefined, restored: RestoredSaveSelection, selection: Selection | null): SaveRequest {
  const entryId = getEntryId(entry);
  const fallbackId = entry?.save?.id ?? "";
  const initial = restored.entryId === entryId ? restored : { entryId, saveId: fallbackId };
  const active = selection?.entryId === entryId ? selection : null;
  return { entryId, anchorId: initial.saveId || fallbackId, offset: active ? active.offset : initial.offset,
    saveId: active ? active.saveId : initial.saveId, edge: active?.edge };
}

function resolveSave(page: SavePage | null, request: SaveRequest) {
  if (!page) { return { index: 0, save: undefined, hasPrevious: false, hasNext: false }; }
  const requestedId = request.edge ? "" : request.saveId || request.anchorId;
  const found = page.items.findIndex((save) => save.id === requestedId);
  const index = found >= 0 ? found : request.edge === "last" ? page.items.length - 1 : 0;
  const save = page.items[index];
  return { index, save, hasPrevious: !!save && page.offset + index > 0, hasNext: !!save && page.offset + index + 1 < page.total };
}

async function loadSavePage(gameId: string, saveId: string, requestedOffset?: number): Promise<SavePage> {
  let offset = requestedOffset ?? 0;
  // Locate an explicitly restored save; navigation requests only one page.
  for (;;) {
    const page = result(await api.GET("/api/v1/saves", { params: { query: { gameId, offset, limit: PAGE_SIZE } } }));
    if (requestedOffset !== undefined || !saveId || page.items.some((save) => save.id === saveId) || !page.items.length || offset + page.items.length >= page.total) {
      return page;
    }
    offset += page.items.length;
  }
}

function useGameSavePage(gameId: string | undefined, anchorId: string, offset: number | undefined) {
  return useResource(useCallback(async () => gameId ? loadSavePage(gameId, anchorId, offset) : null, [gameId, anchorId, offset]));
}

export function useSaveSelection(entry: ImmersiveEntry | undefined, enabled: boolean, restored: RestoredSaveSelection) {
  const [selection, setSelection] = useState<Selection | null>(null);
  const { entryId, anchorId, offset, ...requested } = saveRequest(entry, restored, selection);
  const gameId = enabled && entry ? entry.game.id : undefined;
  const resource = useGameSavePage(gameId, anchorId, offset);
  const page = resource.data;
  const resolved = resolveSave(page, { entryId, anchorId, offset, ...requested });
  const { index, save } = resolved;
  // The grouped game list already owns this screenshot. Keep it on screen while
  // reading the full save page, but only a resolved save may be launched.
  const representative = enabled && !requested.edge && (requested.saveId || anchorId) === entry?.save?.id ? entry?.save : undefined;
  const previewSave = page ? save : representative;

  function select(saveId: string) {
    if (!page?.items.some((item) => item.id === saveId)) { return; }
    setSelection({ entryId, offset, saveId });
  }
  function move(direction: "left" | "right") {
    if (!page || !save) { return; }
    const next = index + (direction === "left" ? -1 : 1);
    if (next >= 0 && next < page.items.length) { select(page.items[next].id); return; }
    if (next < 0 && page.offset > 0) { setSelection({ entryId, offset: Math.max(0, page.offset - PAGE_SIZE), edge: "last" }); }
    if (next >= page.items.length && page.offset + page.items.length < page.total) { setSelection({ entryId, offset: page.offset + page.items.length, edge: "first" }); }
  }
  const requestKey = JSON.stringify([gameId, anchorId, offset]);
  return { resource, requestKey, ...resolved, previewSave, select, move };
}
export type ImmersiveSaveSelection = ReturnType<typeof useSaveSelection>;
