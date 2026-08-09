import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { BIOSManager } from "./bios-manager";
import type { BIOSRequirement } from "./runtime-dependencies";

const navigation = vi.hoisted(() => ({ refresh: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => navigation }));

const item = (id: string, overrides: Partial<BIOSRequirement> = {}): BIOSRequirement => ({
  id, coreId: "mgba", coreName: "mGBA", coreArtifactId: "artifact", logicalName: `${id}.bin`,
  sourceKind: "STATIC", requirementMode: "REQUIRED", enabled: true, version: 1, status: "MATCHED", ...overrides,
});

describe("BIOSManager", () => {
  beforeEach(() => window.history.replaceState({ marker: "keep" }, "", "/admin/bios"));
  afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

  it("switches between library requirements and the full catalog without a document navigation", async () => {
    const user = userEvent.setup();
    const library = [item("gba_bios")];
    const catalog = [...library, item("gb_bios", { coreId: "gambatte", coreName: "Gambatte", requirementMode: "OPTIONAL", status: "OPTIONAL_MISSING" })];
    render(<BIOSManager libraryItems={library} catalogItems={catalog} />);

    expect(screen.getByText("gba_bios.bin")).toBeInTheDocument();
    expect(screen.queryByText("gb_bios.bin")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /完整 BIOS 目录/ }));
    expect(screen.getByText("gb_bios.bin")).toBeInTheDocument();
    expect(window.location.search).toBe("?scope=FULL_CATALOG");
    expect(window.history.state).toEqual({ marker: "keep" });
  });

  it("prioritizes blockers and filters immediately by search and quick state", async () => {
    const user = userEvent.setup();
    const items = [item("ready"), item("missing", { status: "MISSING" }), item("warning", { status: "HASH_WARNING" })];
    render(<BIOSManager libraryItems={items} catalogItems={items} />);

    expect(within(screen.getByRole("table", { name: "需要处理的 BIOS 文件" })).getAllByRole("row")).toHaveLength(2);
    await user.click(screen.getByRole("button", { name: /需要处理 2/ }));
    expect(screen.queryByText("ready.bin")).not.toBeInTheDocument();
    await user.type(screen.getByRole("searchbox", { name: "搜索 BIOS 文件" }), "warning");
    expect(screen.getByText("warning.bin")).toBeInTheDocument();
    expect(screen.queryByText("missing.bin")).not.toBeInTheDocument();
  });

  it("shows expected and current MD5 values without a disclosure control", () => {
    render(<BIOSManager libraryItems={[item("gba_bios", {
      expectedMd5: "a860e8c0b6d573d191e4ec7db1b1e4f6",
      activeInstallation: { id: "installation", md5: "a860e8c0b6d573d191e4ec7db1b1e4f6", sha1: "b".repeat(40), sha256: "f".repeat(64), validatedRequirementVersion: 1, createdAtMs: 1 },
    })]} catalogItems={[]} />);

    expect(screen.getByText("期望 MD5")).toBeVisible();
    expect(screen.getByText("当前 MD5")).toBeVisible();
    expect(screen.getAllByText("a860e8c0b6d573d191e4ec7db1b1e4f6")).toHaveLength(2);
    expect(screen.queryByText("校验信息")).not.toBeInTheDocument();
  });

  it("opens a DAT-to-ZIP entry comparison from an installed arcade BIOS filename", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      requirementId: "stvbios",
      logicalName: "stvbios.zip",
      installationId: "installation",
      installationStatus: "MATCHED",
      entries: [
        { status: "ALIASED", expected: { name: "epr19730.ic8", sizeBytes: 524288, crc32: "d0e0889d" }, actual: { name: "epr-19730.ic8", sizeBytes: 524288, crc32: "d0e0889d" } },
        { status: "MISSING", expected: { name: "mpr19754.ic14", sizeBytes: 524288, crc32: "f7722da3" }, actual: null },
        { status: "EXTRA", expected: null, actual: { name: "readme.txt", sizeBytes: 12, crc32: "12345678" } },
      ],
    }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    const bios = item("stvbios", {
      coreId: "mame2003_plus",
      coreName: "MAME 2003-Plus",
      logicalName: "stvbios.zip",
      sourceKind: "DAT_MACHINE",
      activeInstallation: { id: "installation", md5: "a".repeat(32), sha1: "b".repeat(40), sha256: "f".repeat(64), validatedRequirementVersion: 1, createdAtMs: 1 },
    });
    render(<BIOSManager libraryItems={[bios]} catalogItems={[bios]} />);

    await user.click(screen.getByRole("button", { name: "stvbios.zip" }));
    const dialog = await screen.findByRole("alertdialog", { name: "stvbios.zip 内容对比" });
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/bios/stvbios/entries", { credentials: "same-origin" });
    const expectedList = within(dialog).getByRole("list", { name: "DAT 要求列表" });
    const actualList = within(dialog).getByRole("list", { name: "当前 ZIP 内容列表" });
    expect(expectedList.querySelectorAll(":scope > li")).toHaveLength(2);
    expect(actualList.querySelectorAll(":scope > li")).toHaveLength(2);
    const expectedHeader = within(dialog).getByRole("list", { name: "DAT 要求字段表头" });
    expect(within(expectedHeader).getAllByRole("listitem")).toHaveLength(3);
    expect(within(expectedHeader).getAllByRole("listitem").map((field) => field.textContent)).toEqual(["name", "size", "crc"]);
    const expectedFacts = within(expectedList).getByRole("list", { name: "epr19730.ic8 文件信息" });
    expect(within(expectedFacts).getAllByRole("listitem")).toHaveLength(3);
    expect(within(expectedFacts).queryByText("name")).not.toBeInTheDocument();
    expect(within(expectedFacts).getByText("524,288 bytes")).toBeVisible();
    expect(within(expectedList).getByText("epr19730.ic8")).toBeVisible();
    expect(within(expectedList).queryByText("readme.txt")).not.toBeInTheDocument();
    expect(within(actualList).getByText("epr-19730.ic8")).toBeVisible();
    expect(within(actualList).getByText("readme.txt")).toBeVisible();
    expect(within(dialog).getAllByText("内容匹配·文件名不同")).toHaveLength(2);
    expect(within(expectedList).getByText("ZIP 内缺失")).toBeVisible();
    expect(within(actualList).getByText("ZIP 内额外文件")).toBeVisible();
  });
});
