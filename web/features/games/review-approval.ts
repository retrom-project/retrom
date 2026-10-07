import { ApiError } from "@/lib/api/client";
import type { Game, Schema } from "@/lib/api/types";

export type ReviewCandidate = Pick<Game, "id" | "title" | "version">;
export type ReviewReadiness = Schema<"ReviewReadiness">;
export type ReviewApprovalSummary = {
  total: number;
  checked: number;
  approved: number;
  missingBios: number;
  failed: number;
  interrupted: boolean;
  failures: Array<{ game: ReviewCandidate; message: string }>;
  missingBiosDetails: Array<{ game: ReviewCandidate; requirements: ReviewReadiness["missingBios"] }>;
};
type ReviewPage = { items: ReviewCandidate[]; total: number };
type ApprovalActions = {
  readiness: (games: ReviewCandidate[]) => Promise<ReviewReadiness[]>;
  approve: (game: ReviewCandidate) => Promise<void>;
};

/** Collect before publishing, so our own removals never shift later pages. */
export async function reviewSnapshot(load: (offset: number) => Promise<ReviewPage>, signal: AbortSignal) {
  const games = new Map<string, ReviewCandidate>();
  let total = Infinity;
  for (let offset = 0; offset < total && !signal.aborted; offset += 100) {
    if (offset > 100000) { throw new Error("匹配的待审游戏过多，请缩小筛选范围后重试。"); }
    const page = await load(offset);
    total = Math.min(total, page.total);
    for (const game of page.items) { games.set(game.id, game); }
    if (page.items.length < 100) { break; }
  }
  return [...games.values()];
}

export async function approveReviewSnapshot(
  games: ReviewCandidate[],
  actions: ApprovalActions,
  signal: AbortSignal,
  progress: (summary: ReviewApprovalSummary) => void,
) {
  const summary: ReviewApprovalSummary = {
    total: games.length, checked: 0, approved: 0, missingBios: 0, failed: 0, interrupted: false, failures: [], missingBiosDetails: [],
  };
  const publish = () => progress({ ...summary, failures: summary.failures.slice(0, 20), missingBiosDetails: summary.missingBiosDetails.slice(0, 20) });
  function failed(game: ReviewCandidate, failure: unknown) {
    summary.failed++;
    summary.failures.push({ game, message: failure instanceof Error ? failure.message : "审批失败，请重试。" });
    if (failure instanceof ApiError && [401, 403].includes(failure.status)) { summary.interrupted = true; }
  }
  async function batch(items: ReviewCandidate[]) {
    let readiness: ReviewReadiness[];
    try { readiness = await actions.readiness(items); }
    catch (failure) {
      for (const game of items) { failed(game, failure); }
      summary.checked += items.length;
      publish();
      return;
    }
    const byId = new Map(readiness.map((item) => [item.id, item]));
    let next = 0;
    async function worker() {
      while (next < items.length && !signal.aborted && !summary.interrupted) {
        const game = items[next++];
        try {
          const readiness = byId.get(game.id);
          if (eligible(game, readiness)) {
            await actions.approve(game);
            summary.approved++;
          } else {
            summary.missingBios++;
            summary.missingBiosDetails.push({ game, requirements: readiness!.missingBios });
          }
        } catch (failure) { failed(game, failure); }
        summary.checked++;
        if (summary.checked % 10 === 0 || summary.checked === summary.total) { publish(); }
      }
    }
    await Promise.all(Array.from({ length: Math.min(3, items.length) }, worker));
  }
  publish();
  for (let offset = 0; offset < games.length && !signal.aborted && !summary.interrupted; offset += 100) {
    await batch(games.slice(offset, offset + 100));
  }
  summary.interrupted ||= signal.aborted;
  publish();
  return summary;
}

function eligible(game: ReviewCandidate, readiness: ReviewReadiness | undefined) {
  if (!readiness || readiness.error || readiness.biosSatisfied === null) {
    throw new Error(readiness?.error?.message || "无法检查所需 BIOS，请重试。");
  }
  if (readiness.version !== game.version) { throw new Error("游戏资料已更新，请重新检查后重试。"); }
  return readiness.biosSatisfied;
}
