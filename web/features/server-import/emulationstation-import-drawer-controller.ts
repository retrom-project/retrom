"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api/client";
import { responseError } from "@/lib/upload";
import {
  type EmulationStationCollection,
  type EmulationStationDirectory,
  type EmulationStationGamelist,
  type EmulationStationImportSummary,
} from "./emulationstation-import-model";
import type { EmulationStationMappingDraft } from "./emulationstation-import-drawer-view";

async function responseMessage(response: Response, fallback: string) {
  return responseError(response, fallback);
}

async function loadPlanCollections(planId: string) {
  const items: EmulationStationCollection[] = [];
  let cursor: string | undefined;
  do {
    const { data, response } = await api.GET(
      "/api/v1/admin/emulationstation-imports/{emulationStationImportId}/collections",
      { params: { path: { emulationStationImportId: planId }, query: { cursor, limit: 100 } } },
    );
    if (!data) {
      throw new Error(await responseMessage(response, "Gamelist 映射读取失败"));
    }
    items.push(...data.items);
    cursor = data.nextCursor ?? undefined;
  } while (cursor);
  return items;
}

async function loadPlanGamelists(planId: string) {
  const items: EmulationStationGamelist[] = [];
  let cursor: string | undefined;
  do {
    const { data, response } = await api.GET(
      "/api/v1/admin/emulationstation-imports/{emulationStationImportId}/gamelists",
      { params: { path: { emulationStationImportId: planId }, query: { cursor, limit: 100 } } },
    );
    if (!data) {
      throw new Error(await responseMessage(response, "Gamelist 清单读取失败"));
    }
    items.push(...data.items);
    cursor = data.nextCursor ?? undefined;
  } while (cursor);
  return items;
}

type PlanControllerOptions = {
  open: boolean;
  resumablePlan?: EmulationStationImportSummary;
  onError: (message: string) => void;
  onStep: (step: 1 | 2 | 3) => void;
  onSnapshotHydrated: () => void;
};

export function useEmulationStationPlanController({
  open,
  resumablePlan,
  onError,
  onStep,
  onSnapshotHydrated,
}: PlanControllerOptions) {
  const hydratedPlanId = useRef("");
  const refreshPromise = useRef<Promise<EmulationStationImportSummary> | null>(null);
  const [plan, setPlan] = useState<EmulationStationImportSummary | null>(resumablePlan ?? null);
  const [gamelists, setGamelists] = useState<EmulationStationGamelist[]>([]);
  const [collections, setCollections] = useState<EmulationStationCollection[]>([]);
  const [mappings, setMappings] = useState<Record<string, EmulationStationMappingDraft>>({});

  const hydrateSnapshot = useCallback(async (planId: string) => {
    const [loadedCollections, loadedGamelists] = await Promise.all([
      loadPlanCollections(planId),
      loadPlanGamelists(planId),
    ]);
    setCollections(loadedCollections);
    setGamelists(loadedGamelists);
    onSnapshotHydrated();
    setMappings(Object.fromEntries(loadedCollections.map((collection) => [collection.id, {
      action: collection.mappingAction ?? "",
      platformInstanceId: collection.targetPlatformInstanceId ?? "",
      tags: collection.tagSnapshot,
    }] as const)));
    return loadedCollections;
  }, [onSnapshotHydrated]);

  const refreshPlan = useCallback((planId: string) => {
    if (refreshPromise.current) {
      return refreshPromise.current;
    }
    const promise = (async () => {
      const { data, response } = await api.GET(
        "/api/v1/admin/emulationstation-imports/{emulationStationImportId}",
        { params: { path: { emulationStationImportId: planId } } },
      );
      if (!data) {
        throw new Error(await responseMessage(response, "EmulationStation 计划读取失败"));
      }
      setPlan(data);
      if (data.state === "AWAITING_MAPPING") {
        const loaded = await hydrateSnapshot(data.id);
        const complete = loaded.length > 0 && loaded.every((item) => (
          item.mappingAction === "SKIP"
          || item.mappingAction === "IMPORT" && Boolean(item.targetPlatformInstanceId)
        ));
        onStep(complete ? 3 : 2);
      }
      return data;
    })();
    refreshPromise.current = promise;
    void promise.finally(() => {
      if (refreshPromise.current === promise) {
        refreshPromise.current = null;
      }
    }).catch(() => undefined);
    return promise;
  }, [hydrateSnapshot, onStep]);

  useEffect(() => {
    const planId = resumablePlan?.id ?? "";
    if (!open) {
      hydratedPlanId.current = "";
      return;
    }
    if (!planId || hydratedPlanId.current === planId) {
      return;
    }
    hydratedPlanId.current = planId;
    queueMicrotask(() => void refreshPlan(planId).catch((caught: unknown) => {
      onError(caught instanceof Error ? caught.message : "EmulationStation 计划读取失败");
    }));
  }, [onError, open, refreshPlan, resumablePlan?.id]);

  useEffect(() => {
    if (!open || !plan || plan.state !== "SCANNING") {
      return;
    }
    const update = () => void refreshPlan(plan.id).catch((caught: unknown) => {
      onError(caught instanceof Error ? caught.message : "扫描进度读取失败");
    });
    const timer = window.setInterval(update, 2_000);
    const source = typeof EventSource === "undefined" ? null : new EventSource(
      `/api/v1/admin/jobs/${encodeURIComponent(plan.scanJobId)}/events`,
      { withCredentials: true },
    );
    for (const event of ["progress", "succeeded", "failed"]) {
      source?.addEventListener(event, update);
    }
    return () => {
      window.clearInterval(timer);
      source?.close();
    };
  }, [onError, open, plan, refreshPlan]);

  const acceptCreatedPlan = useCallback((created: EmulationStationImportSummary) => {
    hydratedPlanId.current = created.id;
    setPlan(created);
  }, []);

  return {
    plan,
    setPlan,
    gamelists,
    collections,
    mappings,
    setMappings,
    acceptCreatedPlan,
  };
}

type DirectoryControllerOptions = {
  open: boolean;
  step: 1 | 2 | 3;
  rootId: string;
  path: string;
  onError: (message: string) => void;
};

export function useEmulationStationDirectories({
  open,
  step,
  rootId,
  path,
  onError,
}: DirectoryControllerOptions) {
  const [directories, setDirectories] = useState<EmulationStationDirectory[]>([]);
  const [directoryCursor, setDirectoryCursor] = useState<string | null>(null);
  const [directoryLoading, setDirectoryLoading] = useState(false);

  useEffect(() => {
    if (!open || step !== 1 || !rootId) {
      return;
    }
    const controller = new AbortController();
    queueMicrotask(() => setDirectoryLoading(true));
    void api.GET(
      "/api/v1/admin/server-import-roots/{rootId}/directories",
      { params: { path: { rootId }, query: { path, limit: 100 } }, signal: controller.signal },
    ).then(async ({ data, response }) => {
      if (!data) {
        throw new Error(await responseMessage(response, "服务器目录读取失败"));
      }
      setDirectories(data.items);
      setDirectoryCursor(data.nextCursor);
    }).catch((caught: unknown) => {
      if (!(caught instanceof DOMException && caught.name === "AbortError")) {
        onError(caught instanceof Error ? caught.message : "服务器目录读取失败");
      }
    }).finally(() => setDirectoryLoading(false));
    return () => controller.abort();
  }, [onError, open, path, rootId, step]);

  const loadMoreDirectories = useCallback(async () => {
    if (!directoryCursor || directoryLoading) {
      return;
    }
    setDirectoryLoading(true);
    onError("");
    try {
      const { data, response } = await api.GET(
        "/api/v1/admin/server-import-roots/{rootId}/directories",
        { params: { path: { rootId }, query: { path, cursor: directoryCursor, limit: 100 } } },
      );
      if (!data) {
        throw new Error(await responseMessage(response, "服务器目录读取失败"));
      }
      setDirectories((current) => [
        ...current,
        ...data.items.filter((item) => !current.some((known) => known.relativePath === item.relativePath)),
      ]);
      setDirectoryCursor(data.nextCursor);
    } catch (caught) {
      onError(caught instanceof Error ? caught.message : "服务器目录读取失败");
    } finally {
      setDirectoryLoading(false);
    }
  }, [directoryCursor, directoryLoading, onError, path, rootId]);

  return {
    directories,
    directoryCursor,
    directoryLoading,
    loadMoreDirectories,
  };
}
