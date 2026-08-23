"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import type { TagReference } from "@/components/tag-picker";
import { api, writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import { responseError } from "@/lib/upload";
import { EmulationStationImportDetailView, type EmulationStationDetailFilters } from "./emulationstation-import-detail-view";
import { EmulationStationImportDrawer } from "./emulationstation-import-manager";
import type {
  EmulationStationCollection,
  EmulationStationGamelist,
  EmulationStationImportSummary,
  EmulationStationItemList,
  EmulationStationPlatformInstance,
} from "./emulationstation-import-model";
import type { ServerImportRoot } from "./server-import-manager";

function message(response: Response, fallback: string) {
  return responseError(response, fallback);
}

type EmulationStationImportDetailManagerProps = {
  initialSummary: EmulationStationImportSummary;
  initialItems: EmulationStationItemList;
  collections: EmulationStationCollection[];
  gamelists: EmulationStationGamelist[];
  roots: ServerImportRoot[];
  platformInstances: EmulationStationPlatformInstance[];
  activeTags?: TagReference[];
  initialFilters: EmulationStationDetailFilters;
};

export function EmulationStationImportDetailManager({
  initialSummary,
  initialItems,
  collections,
  gamelists,
  roots,
  platformInstances,
  activeTags = [],
  initialFilters,
}: EmulationStationImportDetailManagerProps) {
  const router = useRouter();
  const [summary, setSummary] = useState(initialSummary);
  const [items, setItems] = useState(initialItems.items);
  const [nextCursor, setNextCursor] = useState(initialItems.nextCursor);
  const [filters, setFilters] = useState(initialFilters);
  const [draft, setDraft] = useState(initialFilters);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [cancelOpen, setCancelOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [mappingOpen, setMappingOpen] = useState(false);

  const requestSummary = useCallback(async () => {
    const { data, response } = await api.GET(
      "/api/v1/admin/emulationstation-imports/{emulationStationImportId}",
      { params: { path: { emulationStationImportId: initialSummary.id } } },
    );
    if (!data) {
      throw new Error(await message(response, "任务摘要读取失败"));
    }
    setSummary(data);
  }, [initialSummary.id]);

  const requestItems = useCallback(async (active: EmulationStationDetailFilters, cursor?: string, append = false) => {
    const { data, response } = await api.GET(
      "/api/v1/admin/emulationstation-imports/{emulationStationImportId}/items",
      {
        params: {
          path: { emulationStationImportId: initialSummary.id },
          query: {
            q: active.query || undefined,
            outcome: active.outcome || undefined,
            warning: active.warning || undefined,
            collectionId: active.collectionId || undefined,
            cursor,
            limit: 50,
          },
        },
      },
    );
    if (!data) {
      throw new Error(await message(response, "任务结果读取失败"));
    }
    setItems((current) => append
      ? [...current, ...data.items.filter((item) => !current.some((known) => known.id === item.id))]
      : data.items);
    setNextCursor(data.nextCursor);
  }, [initialSummary.id]);

  useEffect(() => {
    const values = new URLSearchParams(window.location.search);
    const updates = {
      q: filters.query,
      outcome: filters.outcome,
      warning: filters.warning,
      collectionId: filters.collectionId,
    };
    for (const [name, value] of Object.entries(updates)) {
      if (value) {
        values.set(name, value);
      } else {
        values.delete(name);
      }
    }
    const encoded = values.toString();
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${encoded ? `?${encoded}` : ""}`);
  }, [filters]);

  useEffect(() => {
    if (!["SCANNING", "QUEUED", "RUNNING", "CANCEL_REQUESTED"].includes(summary.state)) {
      return;
    }
    const update = () => {
      void requestSummary().catch(() => undefined);
      void requestItems(filters).catch(() => undefined);
    };
    const timer = window.setInterval(update, 4_000);
    const jobId = summary.importJobId ?? summary.scanJobId;
    const source = typeof EventSource === "undefined" ? null : new EventSource(
      `/api/v1/admin/jobs/${encodeURIComponent(jobId)}/events`,
      { withCredentials: true },
    );
    for (const event of ["progress", "succeeded", "failed", "cancelled"]) {
      source?.addEventListener(event, update);
    }
    return () => {
      window.clearInterval(timer);
      source?.close();
    };
  }, [filters, requestItems, requestSummary, summary.importJobId, summary.scanJobId, summary.state]);

  async function applyFilters() {
    setBusy(true);
    setError("");
    try {
      setFilters(draft);
      await requestItems(draft);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "任务结果读取失败");
    } finally {
      setBusy(false);
    }
  }

  async function cancel() {
    setBusy(true);
    setError("");
    try {
      const { data, response } = await api.POST(
        "/api/v1/admin/emulationstation-imports/{emulationStationImportId}/cancel",
        {
          params: {
            path: { emulationStationImportId: summary.id },
            header: {
              ...writeHeaders(),
              "If-Match": `"v${summary.version}"`,
              "Idempotency-Key": newUuid(),
              "X-Retrom-Csrf": "",
            },
          },
          body: { reason: "管理员停止 EmulationStation 导入" },
        },
      );
      if (!data) {
        throw new Error(await message(response, "取消任务失败"));
      }
      setSummary(data);
      setCancelOpen(false);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "取消任务失败");
    } finally {
      setBusy(false);
    }
  }

  async function retry() {
    setBusy(true);
    setError("");
    try {
      const { data, response } = await api.POST(
        "/api/v1/admin/emulationstation-imports/{emulationStationImportId}/retry",
        {
          params: {
            path: { emulationStationImportId: summary.id },
            header: {
              ...writeHeaders(),
              "If-Match": `"v${summary.version}"`,
              "Idempotency-Key": newUuid(),
              "X-Retrom-Csrf": "",
            },
          },
          body: {},
        },
      );
      if (!data) {
        throw new Error(await message(response, "重试任务失败"));
      }
      setSummary(data);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "重试任务失败");
    } finally {
      setBusy(false);
    }
  }

  async function deletePlan() {
    setBusy(true);
    setError("");
    try {
      const { response } = await api.DELETE(
        "/api/v1/admin/emulationstation-imports/{emulationStationImportId}",
        {
          params: {
            path: { emulationStationImportId: summary.id },
            header: {
              ...writeHeaders(),
              "If-Match": `"v${summary.version}"`,
              "Idempotency-Key": newUuid(),
              "X-Retrom-Csrf": "",
            },
          },
        },
      );
      if (!response.ok) {
        throw new Error(await message(response, "删除计划失败"));
      }
      router.push("/admin/imports/server");
      router.refresh();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "删除计划失败");
      setBusy(false);
    }
  }

  function loadMore() {
    if (!nextCursor || busy) {
      return;
    }
    setBusy(true);
    void requestItems(filters, nextCursor, true)
      .catch((caught: unknown) => setError(caught instanceof Error ? caught.message : "加载失败"))
      .finally(() => setBusy(false));
  }

  const mappingDrawer = <EmulationStationImportDrawer
    open
    roots={roots}
    platformInstances={platformInstances}
    activeTags={activeTags}
    resumablePlan={summary}
    onClose={() => setMappingOpen(false)}
    onStarted={setSummary}
  />;
  return <EmulationStationImportDetailView
    summary={summary}
    items={items}
    nextCursor={nextCursor}
    draft={draft}
    collections={collections}
    gamelists={gamelists}
    busy={busy}
    error={error}
    cancelOpen={cancelOpen}
    deleteOpen={deleteOpen}
    mappingOpen={mappingOpen}
    mappingDrawer={mappingDrawer}
    onDraft={setDraft}
    onApplyFilters={() => void applyFilters()}
    onCancelOpen={setCancelOpen}
    onDeleteOpen={setDeleteOpen}
    onCancel={() => void cancel()}
    onDelete={() => void deletePlan()}
    onRetry={() => void retry()}
    onMappingOpen={setMappingOpen}
    onLoadMore={loadMore}
    onDismissError={() => setError("")}
  />;
}
