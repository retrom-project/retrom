import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import type { Schema } from "../lib/api/types";
import type { SaveDraft } from "../features/player/save-drafts";
import {
  leavePlayer,
  request,
  revealPlayerControls,
} from "./clean-refactor-support";
import { nesFrame, sendNesInput } from "./nes-playback";

export async function newInstantSave(page: Page, existing: Schema<"Save">) {
  await revealPlayerControls(page);
  const persisted = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/v1/saves" &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await page
    .getByRole("dialog", { name: "保存存档", exact: true })
    .getByRole("button", { name: "新建存档", exact: true })
    .click();
  const response = await persisted;
  expect(response.status()).toBe(200);
  const save = (await response.json()) as Schema<"Save">;
  expect(save.id).not.toBe(existing.id);
  expect(save.version).toBe(1);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByRole("status").filter({ hasText: "存档已同步。" }),
  ).toBeVisible();
  return save;
}

async function overwrite(page: Page, save: Schema<"Save">) {
  await revealPlayerControls(page);
  const persisted = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === `/api/v1/saves/${save.id}` &&
      response.request().method() === "PUT",
  );
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await page
    .getByRole("dialog", { name: "保存存档", exact: true })
    .getByRole("button", { name: "覆盖当前存档", exact: true })
    .click();
  return persisted;
}

export async function overwriteInstantSave(page: Page, save: Schema<"Save">) {
  const response = await overwrite(page, save);
  expect(response.status()).toBe(200);
  const updated = (await response.json()) as Schema<"Save">;
  expect(updated.id).toBe(save.id);
  expect(updated.name).toBe(save.name);
  expect(updated.version).toBe(save.version + 1);
  await expect(
    page.getByRole("status").filter({ hasText: "存档已同步。" }),
  ).toBeVisible();
  return updated;
}

export async function instantConflict(page: Page, save: Schema<"Save">) {
  const other = await page.context().newPage();
  try {
    await other.goto(`/saves?gameId=${save.game.id}`);
    await other
      .locator(".save-library-card")
      .first()
      .getByRole("button", { name: "从这里继续", exact: true })
      .click();
    await expect(other).toHaveURL(/\/play\//u);
    const runId = new URL(other.url()).pathname.split("/").at(-1);
    const run = await request<Schema<"Run">>(other, `/api/v1/runs/${runId}`);
    expect(run.save?.id).toBe(save.id);
    expect(run.save?.version).toBe(save.version);
    const otherFrame = await nesFrame(other);
    await sendNesInput(other, otherFrame);
    const first = await overwrite(page, save);
    expect(first.status()).toBe(200);
    const updated = (await first.json()) as Schema<"Save">;
    expect(updated.id).toBe(save.id);
    expect(updated.name).toBe(save.name);
    expect(updated.version).toBe(save.version + 1);
    const stale = await overwrite(other, save);
    expect(stale.status()).toBe(409);
    expect((await stale.json()) as Schema<"Error">).toMatchObject({
      code: "VERSION_CONFLICT",
    });
    await expect(
      other.getByRole("status").filter({ hasText: "重新同步不会强行覆盖" }),
    ).toBeVisible();
    await expect(
      other.getByRole("button", { name: "重新同步", exact: true }),
    ).toBeVisible();
    const retained = await other.evaluate(
      () =>
        new Promise<
          {
            saveId: SaveDraft["saveId"];
            metadata: SaveDraft["metadata"];
            payloadSize: number;
          }[]
        >((resolve, reject) => {
          const opened = indexedDB.open("retrom-unsynced-saves", 1);
          opened.onerror = () => reject(opened.error);
          opened.onsuccess = () => {
            const database = opened.result;
            const transaction = database.transaction("drafts", "readonly");
            const read = transaction.objectStore("drafts").getAll();
            read.onerror = () => reject(read.error);
            read.onsuccess = () => {
              resolve(
                (read.result as SaveDraft[]).map((record) => ({
                  saveId: record.saveId,
                  metadata: record.metadata,
                  payloadSize: record.payload.size,
                })),
              );
              database.close();
            };
          };
        }),
    );
    const draft = retained.find((record) => record.saveId === save.id);
    expect(draft?.metadata.version).toBe(save.version);
    expect(draft?.metadata.runId).toBe(run.id);
    expect(draft?.metadata.extinfo).toEqual(run.extinfo);
    expect(draft?.payloadSize).toBeGreaterThan(0);
    const current = await request<Schema<"SavePage">>(
      page,
      `/api/v1/saves?gameId=${save.game.id}`,
    );
    expect(current.items.find((record) => record.id === save.id)?.version).toBe(
      updated.version,
    );
    await leavePlayer(other);
  } finally {
    await other.close();
  }
}
