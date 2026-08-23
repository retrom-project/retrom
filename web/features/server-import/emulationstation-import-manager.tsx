"use client";

import { useRouter } from "next/navigation";
import { useCallback, useMemo, useState } from "react";
import type { TagReference } from "@/components/tag-picker";
import { api, writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import { responseError } from "@/lib/upload";
import type { ServerImportRoot } from "./server-import-manager";
import {
  useEmulationStationDirectories,
  useEmulationStationPlanController,
} from "./emulationstation-import-drawer-controller";
import { EmulationStationImportDrawerView } from "./emulationstation-import-drawer-view";
import {
  type EmulationStationCollection,
  type EmulationStationGamelist,
  type EmulationStationImportList,
  type EmulationStationImportSummary,
  type EmulationStationItem,
  type EmulationStationItemList,
  type EmulationStationPlatformInstance,
} from "./emulationstation-import-model";

export {
  type EmulationStationCollection,
  type EmulationStationGamelist,
  type EmulationStationImportList,
  type EmulationStationImportSummary,
  type EmulationStationItem,
  type EmulationStationItemList,
  type EmulationStationPlatformInstance,
};

async function message(response: Response, fallback: string) {
  return responseError(response, fallback);
}

function mergeTags(current: TagReference[], additions: TagReference[]) {
  const merged = new Map(current.map((tag) => [tag.tagId, tag]));
  additions.forEach((tag) => merged.set(tag.tagId, tag));
  return [...merged.values()].sort((left, right) => left.name.localeCompare(right.name, "zh-CN"));
}

type EmulationStationImportDrawerProps = {
  open: boolean;
  roots: ServerImportRoot[];
  platformInstances: EmulationStationPlatformInstance[];
  activeTags?: TagReference[];
  resumablePlan?: EmulationStationImportSummary;
  onClose: () => void;
  onStarted: (summary: EmulationStationImportSummary) => void;
};

export function EmulationStationImportDrawer({
  open,
  roots,
  platformInstances,
  activeTags = [],
  resumablePlan,
  onClose,
  onStarted,
}: EmulationStationImportDrawerProps) {
  const router = useRouter();
  const [step, setStep] = useState<1 | 2 | 3>(resumablePlan ? 2 : 1);
  const [rootId, setRootId] = useState(resumablePlan?.root.id ?? roots.find((root) => root.status === "AVAILABLE")?.id ?? "");
  const [path, setPath] = useState(resumablePlan?.sourceRelativePath ?? "");
  const [batchTags, setBatchTags] = useState<TagReference[]>([]);
  const [batchStatus, setBatchStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const resetBatch = useCallback(() => {
    setBatchTags([]);
    setBatchStatus("");
  }, []);
  const {
    plan,
    setPlan,
    gamelists,
    collections,
    mappings,
    setMappings,
    acceptCreatedPlan,
  } = useEmulationStationPlanController({
    open,
    resumablePlan,
    onError: setError,
    onStep: setStep,
    onSnapshotHydrated: resetBatch,
  });
  const {
    directories,
    directoryCursor,
    directoryLoading,
    loadMoreDirectories,
  } = useEmulationStationDirectories({ open, step, rootId, path, onError: setError });
  const instances = useMemo(() => platformInstances.filter((instance) => instance.enabled), [platformInstances]);
  const breadcrumbs = useMemo(() => path ? path.split("/") : [], [path]);
  const selectedRoot = roots.find((root) => root.id === rootId);

  async function scan() {
    if (!rootId || selectedRoot?.status !== "AVAILABLE") {
      return;
    }
    setBusy(true);
    setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/emulationstation-imports", {
        params: { header: { ...writeHeaders(), "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } },
        body: { rootId, sourceRelativePath: path },
      });
      if (!data) {
        throw new Error(await message(response, "EmulationStation 扫描创建失败"));
      }
      acceptCreatedPlan(data);
      setStep(2);
      onStarted(data);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "EmulationStation 扫描创建失败");
    } finally {
      setBusy(false);
    }
  }

  async function confirmMappings() {
    if (!plan) {
      return;
    }
    setBusy(true);
    setError("");
    try {
      let current = plan;
      const values = collections.map((collection) => mappings[collection.id].action === "SKIP" ? {
        collectionId: collection.id,
        action: "SKIP" as const,
        tagIds: [],
      } : {
        collectionId: collection.id,
        action: "IMPORT" as const,
        platformInstanceId: mappings[collection.id].platformInstanceId,
        tagIds: mappings[collection.id].tags.map((tag) => tag.tagId),
      });
      for (let offset = 0; offset < values.length; offset += 100) {
        const { data, response } = await api.PUT(
          "/api/v1/admin/emulationstation-imports/{emulationStationImportId}/collection-mappings",
          {
            params: {
              path: { emulationStationImportId: current.id },
              header: {
                ...writeHeaders(),
                "If-Match": `"v${current.version}"`,
                "Idempotency-Key": newUuid(),
                "X-Retrom-Csrf": "",
              },
            },
            body: { mappings: values.slice(offset, offset + 100) },
          },
        );
        if (!data) {
          throw new Error(await message(response, "Gamelist 映射保存失败"));
        }
        current = data;
      }
      setPlan(current);
      setStep(3);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Gamelist 映射保存失败");
    } finally {
      setBusy(false);
    }
  }

  function applyBatchTags() {
    if (!batchTags.length) {
      return;
    }
    const targets = collections.filter((item) => mappings[item.id]?.action !== "SKIP");
    if (targets.some((item) => mergeTags(mappings[item.id]?.tags ?? [], batchTags).length > 20)) {
      setError("批量添加后会超过每个 Collection 20 个标签，请先移除部分标签。");
      return;
    }
    setMappings((current) => Object.fromEntries(Object.entries(current).map(([id, draft]) => [
      id,
      draft.action === "SKIP" ? draft : { ...draft, tags: mergeTags(draft.tags, batchTags) },
    ])));
    const taggedGames = targets.reduce((total, item) => total + item.gameCount, 0);
    setBatchStatus(`已追加到 ${targets.length} 个未跳过 Collection，覆盖 ${taggedGames} 个游戏。`);
  }

  async function startImport() {
    if (!plan) {
      return;
    }
    setBusy(true);
    setError("");
    try {
      const { data, response } = await api.POST(
        "/api/v1/admin/emulationstation-imports/{emulationStationImportId}/start",
        {
          params: {
            path: { emulationStationImportId: plan.id },
            header: {
              ...writeHeaders(),
              "If-Match": `"v${plan.version}"`,
              "Idempotency-Key": newUuid(),
              "X-Retrom-Csrf": "",
            },
          },
          body: { version: plan.version },
        },
      );
      if (!data) {
        throw new Error(await message(response, "EmulationStation 导入启动失败"));
      }
      onStarted(data);
      onClose();
      router.push(`/admin/imports/server/emulationstation/${data.id}`);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "EmulationStation 导入启动失败");
    } finally {
      setBusy(false);
    }
  }

  if (!open) {
    return null;
  }
  const mapped = Object.values(mappings).filter((item) => item.action === "IMPORT").length;
  const skipped = Object.values(mappings).filter((item) => item.action === "SKIP").length;
  const tagged = collections.filter((item) => mappings[item.id]?.action === "IMPORT" && mappings[item.id]?.tags.length);
  const mappedTags = mergeTags([], tagged.flatMap((item) => mappings[item.id]?.tags ?? []));
  const mappingComplete = collections.length > 0
    && mapped > 0
    && mapped + skipped === collections.length
    && collections.every((item) => mappings[item.id]?.action !== "IMPORT" || Boolean(mappings[item.id]?.platformInstanceId));
  return <EmulationStationImportDrawerView
    roots={roots}
    rootId={rootId}
    path={path}
    breadcrumbs={breadcrumbs}
    directories={directories}
    directoryCursor={directoryCursor}
    directoryLoading={directoryLoading}
    selectedRoot={selectedRoot}
    step={step}
    plan={plan}
    gamelists={gamelists}
    collections={collections}
    mappings={mappings}
    instances={instances}
    activeTags={activeTags}
    batchTags={batchTags}
    batchStatus={batchStatus}
    busy={busy}
    error={error}
    mapped={mapped}
    skipped={skipped}
    taggedCollections={tagged.length}
    taggedGames={tagged.reduce((total, item) => total + item.gameCount, 0)}
    mappedTags={mappedTags}
    mappingComplete={mappingComplete}
    onRoot={(id) => {
      setRootId(id);
      setPath("");
    }}
    onPath={setPath}
    onMore={() => void loadMoreDirectories()}
    onBatchTags={(tags) => {
      setBatchTags(tags);
      setBatchStatus("");
    }}
    onApplyBatch={applyBatchTags}
    onMapping={(id, draft) => setMappings((current) => ({ ...current, [id]: draft }))}
    onClose={onClose}
    onScan={() => void scan()}
    onConfirm={() => void confirmMappings()}
    onStart={() => void startImport()}
    onDismissError={() => setError("")}
  />;
}
