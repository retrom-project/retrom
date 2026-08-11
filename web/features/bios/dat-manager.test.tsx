import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DATManager, supportsArcadeDATCore } from "./dat-manager";
import type { CoreArtifact, DATVersion } from "./runtime-dependencies";

const navigation = vi.hoisted(() => ({ refresh: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => navigation }));

const artifact: CoreArtifact = { id: "artifact", coreId: "fbneo", coreName: "FinalBurn Neo", emulatorjsVersion: "4.2.3", bundleVersion: "4.2.3", enabled: true, version: 1 };
const item = (id: string, overrides: Partial<DATVersion> = {}): DATVersion => ({
  id, coreId: "fbneo", coreName: "FinalBurn Neo", coreArtifactId: "artifact", source: "BUILTIN",
  compatibilityStatus: "MATCHED", parseStatus: "READY", active: false, machineCount: 100,
  romEntryCount: 500, diskEntryCount: 0, biosSetCount: 3, diffStatus: "READY", version: 1, updatedAtMs: 1_786_000_000_000, ...overrides,
});

describe("DATManager", () => {
  afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

  it("shows current versions separately and opens a bounded upload drawer", async () => {
    const user = userEvent.setup();
    render(<DATManager versions={[item("active", { active: true }), item("candidate", { source: "USER" })]} artifacts={[artifact]} />);

    expect(screen.getByRole("heading", { name: "当前启用" })).toBeInTheDocument();
    expect(within(screen.getByRole("table", { name: "DAT 候选与历史版本" })).getAllByRole("row")).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "上传新目录" }));
    expect(screen.getByRole("dialog", { name: "上传街机数据目录" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "开始上传" })).toBeDisabled();
    await user.upload(screen.getByLabelText("DAT 或 XML 文件"), new File(["<datafile />"], "fbneo.dat", { type: "application/xml" }));
    expect(screen.getByDisplayValue("fbneo.dat")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "开始上传" })).toBeEnabled();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "上传街机数据目录" })).not.toBeInTheDocument();
  });

  it("keeps the DAT-capable core set closed and exposes both FBA2012 targets", async () => {
    expect([
      "fbneo", "mame2003", "mame2003_plus", "fbalpha2012_cps1", "fbalpha2012_cps2",
    ].filter(supportsArcadeDATCore)).toHaveLength(5);
    expect(supportsArcadeDATCore("azahar")).toBe(false);

    const user = userEvent.setup();
    const artifacts: CoreArtifact[] = [
      artifact,
      { ...artifact, id: "mame", coreId: "mame2003", coreName: "MAME 2003" },
      { ...artifact, id: "mame-plus", coreId: "mame2003_plus", coreName: "MAME 2003 Plus" },
      { ...artifact, id: "cps1", coreId: "fbalpha2012_cps1", coreName: "FB Alpha 2012 CPS-1" },
      { ...artifact, id: "cps2", coreId: "fbalpha2012_cps2", coreName: "FB Alpha 2012 CPS-2" },
      { ...artifact, id: "azahar", coreId: "azahar", coreName: "Azahar" },
    ];
    render(<DATManager versions={[]} artifacts={artifacts} />);
    await user.click(screen.getByRole("button", { name: "上传新目录" }));
    const options = within(screen.getByLabelText("目标运行方式")).getAllByRole("option");
    expect(options.map((option) => option.textContent)).toEqual([
      "FinalBurn Neo · bundle 4.2.3",
      "MAME 2003 · bundle 4.2.3",
      "MAME 2003 Plus · bundle 4.2.3",
      "FB Alpha 2012 CPS-1 · bundle 4.2.3",
      "FB Alpha 2012 CPS-2 · bundle 4.2.3",
    ]);
  });

  it("filters candidate history in place and only exposes problem states on demand", async () => {
    const user = userEvent.setup();
    const versions = [item("active", { active: true }), item("history"), item("failed", { source: "USER", parseStatus: "FAILED" })];
    render(<DATManager versions={versions} artifacts={[artifact]} />);

    await user.click(screen.getByRole("button", { name: "需处理 1" }));
    const rows = within(screen.getByRole("table", { name: "DAT 候选与历史版本" })).getAllByRole("row");
    expect(rows).toHaveLength(1);
    expect(rows[0]).toHaveTextContent("解析失败");
    expect(screen.queryByText("技术详情", { exact: true })).not.toBeInTheDocument();
  });

  it("opens one real diff dialog with summary, impact and readable changes", async () => {
    const user = userEvent.setup();
    const active = item("active", { active: true });
    const candidate = item("candidate", { source: "USER", compatibilityStatus: "UNKNOWN" });
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({
      baseDatVersionId: active.id,
      targetDatVersionId: candidate.id,
      summary: {
        schemaVersion: 1,
        machines: { added: 2, removed: 1, changed: 3 },
        romEntries: { added: 4, removed: 0, changed: 1 },
        biosSets: { added: 0, removed: 0, changed: 0 },
        dependencyTargets: { added: 1, removed: 0, changed: 0 },
        warnings: 2,
      },
      impact: { dependentPlatformInstanceCount: 1, variantRevalidationCount: 6 },
      impactDigest: "digest",
      items: [{ section: "MACHINES", change: "CHANGED", key: { machine: "1943" }, before: { parent: "old" }, after: { parent: "1943" } }],
      nextCursor: null,
    }), { status: 200, headers: { "Content-Type": "application/json" } })));
    render(<DATManager versions={[active, candidate]} artifacts={[artifact]} />);

    await user.click(screen.getByRole("button", { name: "查看差异" }));
    await waitFor(() => expect(screen.getByRole("alertdialog", { name: "数据目录差异与运行影响" })).toBeInTheDocument());
    expect(screen.getByText("1 个游戏目录受到影响；6 个游戏运行版本需要重新检查；2 项解析警告。")).toBeInTheDocument();
    expect(screen.getByText("machine: 1943")).toBeInTheDocument();
    expect(screen.queryByText("技术详情", { exact: true })).not.toBeInTheDocument();
  });

  it("keeps actions disabled while diff is running and can regenerate a stale diff", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ jobId: "diff-job", status: "PENDING" }), { status: 202, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    render(<DATManager versions={[item("running", { source: "USER", diffStatus: "RUNNING" }), item("stale", { source: "USER", diffStatus: "STALE" })]} artifacts={[artifact]} />);

    expect(screen.getByRole("button", { name: "差异比对中…" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "启用" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "重新生成差异" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/arcade-dats/stale/diff", expect.objectContaining({ method: "POST" })));
    expect(screen.getAllByRole("button", { name: "差异比对中…" })).toHaveLength(2);
  });
});
