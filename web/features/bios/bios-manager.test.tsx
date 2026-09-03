import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { BIOSManager, type BIOSListResponse, type BIOSRequirement } from "./bios-manager";

const item = (id: string, overrides: Partial<BIOSRequirement> = {}): BIOSRequirement => ({
  id, coreId: "mgba", coreName: "mGBA", providerId: "emulatorjs", targetId: "mgba",
  targetContractSha256: "a".repeat(64), logicalName: `${id}.bin`,
  sourceKind: "STATIC", requirementMode: "REQUIRED", conditionCode: null, expectedMd5: null,
  enabled: true, version: 1, status: "MATCHED", activeInstallation: null, ...overrides,
});

function page(items: BIOSRequirement[], options: { scope?: BIOSListResponse["scope"]; total?: number; next?: string | null } = {}): BIOSListResponse {
  const total = options.total ?? items.length;
  return {
    generatedAtMs: 1,
    scope: options.scope ?? "REQUIRED_BY_LIBRARY",
    scopeCounts: { requiredByLibrary: options.scope === "FULL_CATALOG" ? 8 : total, fullCatalog: 286 },
    summary: {
      totalCount: total,
      blockingCount: items.filter((value) => value.status === "MISSING").length,
      warningCount: items.filter((value) => value.status === "HASH_WARNING").length,
      readyCount: items.filter((value) => value.status === "MATCHED").length,
      attentionCount: items.filter((value) => value.status === "MISSING" || value.status === "HASH_WARNING").length,
      requiredCount: total,
      optionalCount: 0,
    },
    filteredCount: total,
    items,
    nextCursor: options.next ?? null,
  };
}

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
}

function requestedURL(call: unknown[] | undefined) {
  const input = call?.[0];
  return input instanceof Request ? input.url : String(input);
}

describe("BIOSManager", () => {
  beforeEach(() => window.history.replaceState({ marker: "keep" }, "", "/admin/bios"));
  afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

  it("switches scope with one server request and keeps server aggregate counts", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(page([item("catalog")], { scope: "FULL_CATALOG", total: 286 })));
    vi.stubGlobal("fetch", fetchMock);
    render(<BIOSManager initialResponse={page([item("library")], { total: 8 })} />);

    expect(screen.getByText("library.bin")).toBeVisible();
    await user.click(screen.getByRole("button", { name: /完整 BIOS 目录 286/ }));
    expect(await screen.findByText("catalog.bin")).toBeVisible();
    expect(screen.getAllByText("286").length).toBeGreaterThan(0);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(requestedURL(fetchMock.mock.calls[0])).toContain("scope=FULL_CATALOG");
    expect(requestedURL(fetchMock.mock.calls[0])).toContain("limit=100");
    expect(window.location.search).toBe("?scope=FULL_CATALOG");
    expect(window.history.state).toEqual({ marker: "keep" });
  });

  it("loads a fixed 286-item catalog as 100/100/86 without duplicates", async () => {
    const user = userEvent.setup();
    const first = Array.from({ length: 100 }, (_, index) => item(`bios-${index}`));
    const second = Array.from({ length: 100 }, (_, index) => item(`bios-${index + 100}`));
    const third = Array.from({ length: 86 }, (_, index) => item(`bios-${index + 200}`));
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(page(second, { scope: "FULL_CATALOG", total: 286, next: "cursor-2" })))
      .mockResolvedValueOnce(jsonResponse(page(third, { scope: "FULL_CATALOG", total: 286 })));
    vi.stubGlobal("fetch", fetchMock);
    render(<BIOSManager initialScope="FULL_CATALOG" initialResponse={page(first, { scope: "FULL_CATALOG", total: 286, next: "cursor-1" })} />);

    expect(screen.getByText("已加载 100 / 286 项")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "加载更多" }));
    expect(await screen.findByText("已加载 200 / 286 项")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "加载更多" }));
    expect(await screen.findByText("已加载全部 286 项")).toBeVisible();
    expect(screen.getAllByRole("row")).toHaveLength(286);
    expect(screen.getAllByText("bios-99.bin")).toHaveLength(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(requestedURL(fetchMock.mock.calls[0])).toContain("cursor=cursor-1");
    expect(requestedURL(fetchMock.mock.calls[1])).toContain("cursor=cursor-2");
  });

  it("keeps prior pages when a next-page request fails and retries the same cursor", async () => {
    const user = userEvent.setup();
    const initial = Array.from({ length: 100 }, (_, index) => item(`bios-${index}`));
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: { code: "INTERNAL_ERROR" } }, 500))
      .mockResolvedValueOnce(jsonResponse(page([item("bios-100")], { scope: "FULL_CATALOG", total: 101 })));
    vi.stubGlobal("fetch", fetchMock);
    render(<BIOSManager initialScope="FULL_CATALOG" initialResponse={page(initial, { scope: "FULL_CATALOG", total: 101, next: "same-cursor" })} />);

    await user.click(screen.getByRole("button", { name: "加载更多" }));
    expect(await screen.findByRole("button", { name: "重试加载下一页" })).toBeVisible();
    expect(screen.getByText("bios-0.bin")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "重试加载下一页" }));
    expect(await screen.findByText("bios-100.bin")).toBeVisible();
    expect(requestedURL(fetchMock.mock.calls[0])).toContain("cursor=same-cursor");
    expect(requestedURL(fetchMock.mock.calls[1])).toContain("cursor=same-cursor");
  });

  it("shows expected and current MD5 values", () => {
    const bios = item("gba_bios", {
      expectedMd5: "a860e8c0b6d573d191e4ec7db1b1e4f6",
      activeInstallation: { id: "installation", md5: "a860e8c0b6d573d191e4ec7db1b1e4f6", sha1: "b".repeat(40), sha256: "f".repeat(64), validatedRequirementVersion: 1, createdAtMs: 1 },
    });
    render(<BIOSManager initialResponse={page([bios])} />);
    expect(screen.getByText("期望 MD5")).toBeVisible();
    expect(screen.getByText("当前 MD5")).toBeVisible();
    expect(screen.getAllByText("a860e8c0b6d573d191e4ec7db1b1e4f6")).toHaveLength(2);
  });

  it("opens a DAT-to-ZIP entry comparison", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({
      requirementId: "stvbios", logicalName: "stvbios.zip", installationId: "installation", installationStatus: "MATCHED",
      entries: [
        { status: "ALIASED", expected: { name: "epr19730.ic8", sizeBytes: 524288, crc32: "d0e0889d" }, actual: { name: "epr-19730.ic8", sizeBytes: 524288, crc32: "d0e0889d" } },
        { status: "MISSING", expected: { name: "mpr19754.ic14", sizeBytes: 524288, crc32: "f7722da3" }, actual: null },
        { status: "EXTRA", expected: null, actual: { name: "readme.txt", sizeBytes: 12, crc32: "12345678" } },
      ],
    }));
    vi.stubGlobal("fetch", fetchMock);
    const bios = item("stvbios", { coreId: "mame2003_plus", coreName: "MAME 2003-Plus", logicalName: "stvbios.zip", sourceKind: "DAT_MACHINE", activeInstallation: { id: "installation", md5: "a".repeat(32), sha1: "b".repeat(40), sha256: "f".repeat(64), validatedRequirementVersion: 1, createdAtMs: 1 } });
    render(<BIOSManager initialResponse={page([bios])} />);

    await user.click(screen.getByRole("button", { name: "stvbios.zip" }));
    const dialog = await screen.findByRole("alertdialog", { name: "stvbios.zip 内容对比" });
    const expectedList = within(dialog).getByRole("list", { name: "DAT 要求列表" });
    const actualList = within(dialog).getByRole("list", { name: "当前 ZIP 内容列表" });
    expect(expectedList.querySelectorAll(":scope > li")).toHaveLength(2);
    expect(actualList.querySelectorAll(":scope > li")).toHaveLength(2);
    expect(within(expectedList).getByText("epr19730.ic8")).toBeVisible();
    expect(within(actualList).getByText("readme.txt")).toBeVisible();
  });
});
