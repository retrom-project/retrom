import type { Schema } from "@/lib/api/types";
import { api, ApiError, result, upload } from "@/lib/api/client";
import type { SaveDraft } from "./save-drafts";
import { screenshotFileName } from "./screenshot-file";

export class DraftIdentityMismatch extends Error {
  constructor() {
    super(
      "游戏内容、核心或运行配置已变化，无法恢复这份草稿。草稿和原始身份已保留，请导出备份。",
    );
  }
}

export function sameIdentity(left: unknown, right: unknown): boolean {
  if (left === right) {
    return true;
  }
  if (Array.isArray(left) || Array.isArray(right)) {
    return (
      Array.isArray(left) &&
      Array.isArray(right) &&
      left.length === right.length &&
      left.every((value, index) => sameIdentity(value, right[index]))
    );
  }
  if (
    !left ||
    !right ||
    typeof left !== "object" ||
    typeof right !== "object"
  ) {
    return false;
  }
  const leftObject = left as Record<string, unknown>;
  const rightObject = right as Record<string, unknown>;
  const keys = Object.keys(leftObject);
  return (
    keys.length === Object.keys(rightObject).length &&
    keys.every(
      (key) =>
        Object.hasOwn(rightObject, key) &&
        sameIdentity(leftObject[key], rightObject[key]),
    )
  );
}

async function commit(draft: SaveDraft, runId = draft.metadata.runId) {
  const form = new FormData();
  form.set("metadata", JSON.stringify({ ...draft.metadata, runId }));
  form.set("payload", draft.payload, "save.bin");
  if (draft.screenshot) {
    form.set(
      "screenshot",
      draft.screenshot,
      screenshotFileName("screenshot", draft.screenshot),
    );
  }
  const path = draft.saveId ? `/api/v1/saves/${draft.saveId}` : "/api/v1/saves";
  try {
    return await upload<Schema<"Save">>(
      path,
      form,
      draft.saveId ? "PUT" : "POST",
    );
  } catch (failure) {
    if (failure instanceof ApiError && failure.code === "VERSION_CONFLICT") {
      throw new ApiError(
        failure.code,
        "存档已在另一个页面更新。当前草稿已保留，重新同步不会强行覆盖，请先导出备份。",
        failure.status,
      );
    }
    throw failure;
  }
}

async function closeRenewal(runId: string, userId: string) {
  try {
    const response = await api.DELETE("/api/v1/runs/{runId}", {
      params: { path: { runId } },
    });
    if (response.error && response.response.status !== 404) {
      throw new ApiError(
        response.error.code,
        response.error.message,
        response.response.status,
      );
    }
  } catch {
    window.dispatchEvent(
      new CustomEvent("retrom:save-failure", {
        detail: {
          userId,
          message:
            "临时恢复运行未能结束。服务器恢复连接后会自动清理，请保留未同步草稿。",
        },
      }),
    );
  }
}

export async function uploadWithRenewal(draft: SaveDraft) {
  try {
    return await commit(draft);
  } catch (failure) {
    if (!(failure instanceof ApiError) || failure.code !== "CONTEXT_EXPIRED") {
      throw failure;
    }
  }
  const renewed = result(
    await api.POST("/api/v1/runs", {
      body: {
        gameId: draft.gameId,
        purpose: "play",
        coreId: draft.metadata.extinfo.coreId,
        ...(draft.saveId ? { saveId: draft.saveId } : {}),
      },
    }),
  );
  try {
    if (!sameIdentity(draft.metadata.extinfo, renewed.extinfo)) {
      throw new DraftIdentityMismatch();
    }
    return await commit(draft, renewed.id);
  } finally {
    await closeRenewal(renewed.id, draft.userId);
  }
}
