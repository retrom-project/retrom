import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BIOSListResponse } from "./bios-manager";
import {RuntimeAssetPackManager, type RuntimeAssetPackList, type RuntimeTargetList} from "./runtime-asset-pack-manager";
import { RuntimeDependenciesManager } from "./runtime-dependencies-manager";

const definition: RuntimeAssetPackList["definitions"][number] = {
  definitionId: "rpg2000_rtp", kind: "RPG2000_RTP", generation: "RPG2000",
  declaredName: "RPG2000_RTP", normalizedDeclaredName: "rpg2000_rtp", displayName: "RPG Maker 2000 RTP",
  requiredLayoutVersion: "easy-rtp-layout-v1", origin: "BUILTIN", enabled: true,
};

const referenced: RuntimeAssetPackList["installations"][number] = {
  installationId: "01980000-0000-7000-8000-000000000010", definitionId: definition.definitionId,
  filesDigest: "a".repeat(64), fileCount: 2, totalBytes: 4096, bundleSha256: "b".repeat(64),
  status: "READY", diagnostics: [], sourceNote: "licensed media", version: 2, createdAtMs: 1,
  validatedAtMs: 2, deletedAtMs: null, references: { gameCount: 1, checkpointCount: 1 },
};

const emptyBIOS: BIOSListResponse = {
  generatedAtMs: 1, scope: "REQUIRED_BY_LIBRARY",
  scopeCounts: { requiredByLibrary: 0, fullCatalog: 0 },
  summary: { totalCount: 0, blockingCount: 0, warningCount: 0, readyCount: 0, attentionCount: 0, requiredCount: 0, optionalCount: 0 },
  filteredCount: 0, items: [], nextCursor: null,
};

const runtimeTargets: RuntimeTargetList = {items: [{
  providerId: "retrom-runtime", providerVersion: "0.12.0", providerApiVersion: 1,
  bundleSha256: "a".repeat(64), targetId: "rpgmaker-2000", displayName: "RPG Maker 2000",
  coreId: "rpgmaker", coreName: "RPG Maker",
  launchPolicy: "SUPPORTED",
}], nextCursor: null };

beforeEach(() => window.history.replaceState({}, "", "/admin/bios"));
afterEach(() => {cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals();});

describe("RuntimeAssetPackManager", () => {
  it("keeps referenced immutable installations visible and blocks deletion", () => {
    render(<RuntimeAssetPackManager initialList={{ definitions: [definition], installations: [referenced] }} initialRuntimeTargets={runtimeTargets} />);
    expect(screen.getByText("RPG Maker 2000 RTP")).toBeVisible();
    expect(screen.getByText("retrom-runtime/rpgmaker-2000")).toBeVisible();
    expect(screen.getByText("1 款游戏 · 1 个存档")).toBeVisible();
    expect(screen.getByRole("button", { name: "删除" })).toBeDisabled();
  });

  it("opens an upload drawer with archive/directory choice and the legal warning", async () => {
    const user = userEvent.setup();
    render(<RuntimeAssetPackManager initialList={{ definitions: [definition], installations: [] }} initialRuntimeTargets={runtimeTargets} />);
    await user.click(screen.getByRole("button", { name: /安装运行包$/ }));
    expect(screen.getByRole("dialog", { name: "安装 RPG Maker 运行包" })).toBeVisible();
    expect(screen.getByLabelText("ZIP / 7z")).toBeChecked();
    expect(screen.getByLabelText("整个目录")).not.toBeChecked();
    expect(screen.getByText(/Retrom 不提供、下载或重新分发厂商资源/)).toBeVisible();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "安装 RPG Maker 运行包" })).not.toBeInTheDocument();
  });

  it("switches the shared dependency page to the RPG Maker tab and preserves a stable URL", async () => {
    const user = userEvent.setup();
    render(<RuntimeDependenciesManager initialBIOS={emptyBIOS} initialPackList={{ definitions: [definition], installations: [] }} initialRuntimeTargets={runtimeTargets} initialTab="bios" initialScope="REQUIRED_BY_LIBRARY" initialFilters={{ quick: "ALL" }} />);
    await user.click(screen.getByRole("tab", { name: "RPG Maker 运行包" }));
    expect(screen.getByRole("tab", { name: "RPG Maker 运行包" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("heading", { name: "RPG Maker 运行包" })).toBeVisible();
    expect(new URLSearchParams(window.location.search).get("tab")).toBe("rpgmaker");
    await user.keyboard("{ArrowLeft}");
    expect(screen.getByRole("tab", { name: "BIOS 文件" })).toHaveFocus();
    expect(screen.getByRole("tab", { name: "BIOS 文件" })).toHaveAttribute("aria-selected", "true");
    expect(new URLSearchParams(window.location.search).get("tab")).toBe("bios");
  });
});
