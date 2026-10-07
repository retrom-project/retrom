import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/client";
import { approveReviewSnapshot, reviewSnapshot } from "./review-approval";
import type { ReviewCandidate, ReviewReadiness } from "./review-approval";

const games = (count: number): ReviewCandidate[] => Array.from({ length: count }, (_, index) => ({ id: `game-${index}`, title: `游戏 ${index}`, version: 2 }));
const ready = (items: ReviewCandidate[]): ReviewReadiness[] => items.map((game) => ({ id: game.id, version: game.version, biosSatisfied: true, error: null, missingBios: [] }));
const signal = () => new AbortController().signal;

describe("quick review approval", () => {
  it("snapshots every filtered page before any item is published", async () => {
    const pending = games(201);
    const load = vi.fn(async (offset: number) => ({ items: pending.slice(offset, offset + 100), total: pending.length }));
    const snapshot = await reviewSnapshot(load, signal());
    const approve = vi.fn(async (game: ReviewCandidate) => { pending.splice(pending.findIndex((item) => item.id === game.id), 1); });
    const readiness = vi.fn(async (items: ReviewCandidate[]) => ready(items));
    const summary = await approveReviewSnapshot(snapshot, { readiness, approve }, signal(), vi.fn());
    expect(load.mock.calls.map(([offset]) => offset)).toEqual([0, 100, 200]);
    expect(readiness.mock.calls.map(([items]) => items.length)).toEqual([100, 100, 1]);
    expect(summary.approved).toBe(201);
    expect(pending).toEqual([]);
  });

  it("skips missing BIOS while retaining unknown and changed records as failures", async () => {
    const items = games(5);
    const projections = ready(items);
    projections[1].biosSatisfied = false;
    projections[1].missingBios = [{ key: "provider/target/firmware", name: "firmware.rom", coreId: "core" }];
    projections[2].biosSatisfied = null;
    projections[2].error = { code: "CHECK_FAILED", message: "检查失败" };
    projections[3].version = 3;
    projections.pop();
    const approve = vi.fn(async () => undefined);
    const summary = await approveReviewSnapshot(items, { readiness: async () => projections, approve }, signal(), vi.fn());
    expect(approve).toHaveBeenCalledExactlyOnceWith(items[0]);
    expect(summary).toMatchObject({ approved: 1, missingBios: 1, checked: 5, interrupted: false });
    expect(summary.missingBiosDetails).toEqual([{ game: items[1], requirements: projections[1].missingBios }]);
    expect(summary.failures.map(({ game }) => game.id)).toEqual(["game-2", "game-3", "game-4"]);
  });

  it("bounds writes to three concurrent requests and retries only the fresh remaining snapshot", async () => {
    const items = games(8);
    let active = 0;
    let maximum = 0;
    const pending = new Set(items.map((game) => game.id));
    const approve = vi.fn(async (game: ReviewCandidate) => {
      active++;
      maximum = Math.max(maximum, active);
      await new Promise((resolve) => setTimeout(resolve, 0));
      active--;
      if (game.id === "game-2") { throw new ApiError("VERSION_CONFLICT", "version conflict", 409); }
      pending.delete(game.id);
    });
    const summary = await approveReviewSnapshot(items, { readiness: async (batch) => ready(batch), approve }, signal(), vi.fn());
    expect(maximum).toBe(3);
    expect(summary.approved).toBe(7);
    expect(summary.failures).toHaveLength(1);
    const retry = vi.fn(async () => undefined);
    await approveReviewSnapshot(items.filter((game) => pending.has(game.id)), { readiness: async (batch) => ready(batch), approve: retry }, signal(), vi.fn());
    expect(retry).toHaveBeenCalledExactlyOnceWith(items[2]);
  });

  it("stops scheduling after permission loss", async () => {
    const approve = vi.fn(async () => { throw new ApiError("FORBIDDEN", "没有权限", 403); });
    const summary = await approveReviewSnapshot(games(120), { readiness: async (batch) => ready(batch), approve }, signal(), vi.fn());
    expect(approve.mock.calls.length).toBeLessThanOrEqual(3);
    expect(summary.interrupted).toBe(true);
    expect(summary.checked).toBeLessThan(120);
  });

  it("does not publish after navigation cancels a pending readiness read", async () => {
    const controller = new AbortController();
    const approve = vi.fn(async () => undefined);
    const summary = await approveReviewSnapshot(games(3), { readiness: async (batch) => { controller.abort(); return ready(batch); }, approve }, controller.signal, vi.fn());
    expect(approve).not.toHaveBeenCalled();
    expect(summary).toMatchObject({ checked: 0, approved: 0, interrupted: true });
  });

  it("keeps batch read failures distinct from missing BIOS and can continue later batches", async () => {
    const readiness = vi.fn(async (batch: ReviewCandidate[]) => {
      if (batch.length === 100) { throw new Error("连接中断"); }
      return ready(batch);
    });
    const summary = await approveReviewSnapshot(games(101), { readiness, approve: async () => undefined }, signal(), vi.fn());
    expect(summary).toMatchObject({ checked: 101, approved: 1, missingBios: 0 });
    expect(summary.failures).toHaveLength(100);
  });

  it("bounds progress notifications and rendered missing/failed details for a large review queue", async () => {
    let updates = 0;
    let visibleFailures = 0;
    let visibleMissing = 0;
    const summary = await approveReviewSnapshot(games(20000), {
      readiness: async (batch) => ready(batch).map((item, index) => ({ ...item, biosSatisfied: index % 2 === 0 })), approve: async () => { throw new Error("审批失败"); },
    }, signal(), (value) => { updates++; visibleFailures = Math.max(visibleFailures, value.failures.length); visibleMissing = Math.max(visibleMissing, value.missingBiosDetails.length); });
    expect(updates).toBeLessThanOrEqual(2002);
    expect(visibleFailures).toBeLessThanOrEqual(20);
    expect(visibleMissing).toBeLessThanOrEqual(20);
    expect(summary.failed).toBe(10000);
    expect(summary.missingBiosDetails).toHaveLength(10000);
    expect(summary.failures).toHaveLength(10000);
  });
});
