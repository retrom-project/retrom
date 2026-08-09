import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { BIOSManager } from "./bios-manager";
import type { BIOSRequirement } from "./runtime-dependencies";

const navigation = vi.hoisted(() => ({ refresh: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => navigation }));

const item = (id: string, overrides: Partial<BIOSRequirement> = {}): BIOSRequirement => ({
  id, coreId: "mgba", coreName: "mGBA", coreArtifactId: "artifact", logicalName: `${id}.bin`,
  requirementMode: "REQUIRED", enabled: true, version: 1, status: "MATCHED", ...overrides,
});

describe("BIOSManager", () => {
  beforeEach(() => window.history.replaceState({ marker: "keep" }, "", "/admin/bios"));
  afterEach(() => { cleanup(); vi.restoreAllMocks(); });

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
});
