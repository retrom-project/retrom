import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PegasusImportDrawer, type PegasusImportSummary, type PegasusPlatformInstance } from "./pegasus-import-manager";

const router = vi.hoisted(() => ({ push: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));

const root = { id: "games", label: "游戏资料库", status: "AVAILABLE" as const };
const platform: PegasusPlatformInstance = { id: "11111111-1111-4111-8111-111111111111", name: "NES 游戏", platformName: "Nintendo Entertainment System", defaultCoreId: "fceumm", defaultCoreName: "FCEUmm", enabled: true };

function summary(state: PegasusImportSummary["state"], version: number, overrides: Partial<PegasusImportSummary> = {}): PegasusImportSummary {
  return {
    id: "22222222-2222-4222-8222-222222222222", root: { id: root.id, label: root.label }, sourceRelativePath: "Roms/FC", state,
    phase: state === "SCANNING" ? "DISCOVERING_METADATA" : null,
    scanJobId: "33333333-3333-4333-8333-333333333333", importJobId: state === "QUEUED" ? "44444444-4444-4444-8444-444444444444" : null,
    counts: { metadata: 1, invalidMetadata: 0, collections: 1, games: 3, estimatedSourceBytes: 1024, mappedCollections: state === "QUEUED" ? 1 : 0, skippedCollections: 0, processable: 2, blocked: 1, published: 0, existing: 0, failed: 0, cancelled: 0, mediaWarnings: 1, covers: 3, videos: 2 },
    mappingVersion: version, version, createdBy: { id: "55555555-5555-4555-8555-555555555555", displayName: "Admin" }, lastErrorCode: null, retryable: false,
    createdAtMs: 1, updatedAtMs: 2, expiresAtMs: 9999999999999, completedAtMs: null, ...overrides,
  };
}

function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  cleanup();
  router.push.mockReset();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("PegasusImportDrawer", () => {
  it("scans, requires an explicit collection mapping, then starts the frozen plan", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const scanning = summary("SCANNING", 1);
    const awaiting = summary("AWAITING_MAPPING", 2);
    const mapped = summary("AWAITING_MAPPING", 3, { counts: { ...awaiting.counts, mappedCollections: 1 } });
    const queued = summary("QUEUED", 4, { importJobId: "44444444-4444-4444-8444-444444444444", counts: mapped.counts });
    const collection = { id: "66666666-6666-4666-8666-666666666666", metadataRelativePath: "metadata.pegasus.txt", segmentOrdinal: 0, name: "FC", shortName: "nes", description: "", gameCount: 3, issueCount: 1, mappingAction: null, targetPlatformInstanceId: null, targetPlatformInstanceName: null, targetDefaultCoreId: null, targetDefaultCoreName: null, ignoredRules: [], warningFields: [] };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({ rootId: root.id, path: "", items: [{ name: "Roms", relativePath: "Roms" }], nextCursor: null }))
      .mockResolvedValueOnce(json(scanning, 202))
      .mockResolvedValueOnce(json(awaiting))
      .mockResolvedValueOnce(json({ items: [collection], nextCursor: null }))
      .mockResolvedValueOnce(json(mapped))
      .mockResolvedValueOnce(json(queued, 202));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<PegasusImportDrawer open roots={[root]} platformInstances={[platform]} onClose={vi.fn()} onStarted={vi.fn()} />);

    await screen.findByRole("button", { name: /Roms/ });
    await user.click(screen.getByRole("button", { name: "扫描此目录" }));
    expect(await screen.findByText("发现 metadata")).toBeVisible();
    await act(async () => { vi.advanceTimersByTime(2_000); });
    expect(await screen.findByRole("combobox", { name: "FC 处理方式" })).toHaveValue("");
    expect(screen.getByRole("button", { name: "确认映射" })).toBeDisabled();
    await user.selectOptions(screen.getByRole("combobox", { name: "FC 处理方式" }), `IMPORT:${platform.id}`);
    await user.click(screen.getByRole("button", { name: "确认映射" }));
    expect(await screen.findByText("1 个导入 · 0 个跳过")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "开始异步导入" }));

    await waitFor(() => expect(router.push).toHaveBeenCalledWith(`/admin/imports/server/pegasus/${queued.id}`));
    const mappingRequest = fetchMock.mock.calls[4]?.[0] as Request;
    expect(await mappingRequest.clone().json()).toEqual({ mappings: [{ collectionId: collection.id, action: "IMPORT", platformInstanceId: platform.id }] });
    const startRequest = fetchMock.mock.calls[5]?.[0] as Request;
    expect(await startRequest.clone().json()).toEqual({ version: 3 });
  });
});
