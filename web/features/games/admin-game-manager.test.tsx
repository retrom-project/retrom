import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AdminGameManager, type AdminGame, type PlatformInstanceOption } from "./admin-game-manager";

vi.mock("next/navigation", () => ({ useRouter: () => ({ refresh: vi.fn() }) }));

const game: AdminGame = {
  gameId: "game-1",
  status: "PUBLISHED",
  title: "1943",
  description: "Battle of Midway",
  developer: "Capcom",
  publisher: "Capcom",
  genre: "Shoot 'em up",
  players: 2,
  releaseYear: 1987,
  platformId: "arcade",
  platformInstance: { id: "fbneo-games", name: "FBNeo 游戏" },
  currentContentRevisionId: "content-1",
  currentMetadataRevisionId: "metadata-1",
  version: 3,
  createdAtMs: 100,
  updatedAtMs: 200,
  generatedAtMs: 500,
  deleteImpact: { saveStateCount: 44, reviewEventCount: 2, activeLaunchCount: 0 },
  metadataRevisions: [{ id: "metadata-1", sourceKind: "MANUAL", sourceRefId: null, current: true, createdAtMs: 200 }],
  assets: [],
  contentRevisions: [{ id: "content-1", sourceKind: "IMPORT", sourceRefId: "upload-1", current: true, createdAtMs: 150, files: [{ role: "PRIMARY", logicalName: "1943.zip", sortOrder: 0 }] }],
  variants: [{ id: "variant-1", coreId: "fbneo", coreName: "FinalBurn Neo", currentRevisionId: "variant-revision-1", version: 1, revisions: [{ id: "variant-revision-1", contentRevisionId: "content-1", coreArtifactId: "artifact-1", datVersionId: null, status: "READY", compatibilityCode: "READY", current: true, createdAtMs: 180 }] }],
};

const directories: PlatformInstanceOption[] = [
  { id: "fbneo-games", platformId: "arcade", platformName: "Arcade", name: "FBNeo 游戏", defaultCoreId: "fbneo", defaultCoreName: "FinalBurn Neo", enabled: true },
  { id: "neo-geo", platformId: "arcade", platformName: "Arcade", name: "Neo Geo", defaultCoreId: "fbneo", defaultCoreName: "FinalBurn Neo", enabled: true },
];

describe("AdminGameManager", () => {
  afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

  it("renders the precise four-section workbench without the omitted section tags", () => {
    const { container } = render(<AdminGameManager game={game} platformInstances={directories} candidates={[]} />);
    for (const heading of ["发布信息", "媒体", "游戏文件与运行环境", "管理操作", "从游戏库移除"]) {
      expect(screen.getByRole("heading", { name: heading })).toBeInTheDocument();
    }
    for (const omitted of ["媒体资源", "运行状态正常", "维护工具", "危险区域"]) {
      expect(screen.queryByText(omitted, { exact: true })).not.toBeInTheDocument();
    }
    expect(screen.queryByRole("navigation", { name: "游戏管理详情分区" })).not.toBeInTheDocument();
    expect(screen.getByText("从游戏库移除").closest("details")).not.toHaveAttribute("open");
    expect(container.querySelector(".admin-game-cover-slot > .admin-game-cover-frame")).not.toBeNull();
    expect(screen.getByRole("button", { name: "保存新版本" })).toBeDisabled();
  });

  it("enables metadata save only for an unsaved change and disables it after success", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      metadataRevisionId: "metadata-2",
      version: 4,
      updatedAtMs: 600,
    }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    render(<AdminGameManager game={game} platformInstances={directories} candidates={[]} />);
    const save = screen.getByRole("button", { name: "保存新版本" });
    const title = screen.getByRole("textbox", { name: "标题" });

    expect(save).toBeDisabled();
    await user.clear(title);
    await user.type(title, "1943 Kai");
    expect(save).toBeEnabled();
    await user.click(save);

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/games/game-1", expect.objectContaining({
      method: "PATCH",
      body: expect.stringContaining('"title":"1943 Kai"'),
    })));
    await waitFor(() => expect(save).toBeDisabled());
  });

  it("opens a metadata and cover comparison instead of applying text immediately", async () => {
    const user = userEvent.setup();
    render(<AdminGameManager game={game} platformInstances={directories} candidates={[{
      candidateId: "candidate-1", providerGameId: "602921", hitCount: 1,
      metadata: { title: "1941 - Counter Attack", description: "Long provider description", publisher: "HUDSON", releaseYear: 1991 },
      assets: [{ candidateAssetId: "cover-1", kind: "COVER", status: "READY", widthPx: 600, heightPx: 800, mediaType: "image/jpeg" }],
    }]} />);

    expect(screen.queryByRole("button", { name: "采用文字信息" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /对比并选择/ }));
    const dialog = await screen.findByRole("alertdialog", { name: "对比最新游戏信息" });
    const currentColumn = within(dialog).getByRole("region", { name: "当前信息" });
    const latestColumn = within(dialog).getByRole("region", { name: "最新候选" });
    expect(currentColumn).toHaveTextContent("Battle of Midway");
    expect(within(latestColumn).getByText("Long provider description")).toBeVisible();
    expect(within(latestColumn).getByAltText("最新候选封面")).toHaveAttribute("src", expect.stringContaining("cover-1"));
  });

  it("requires an explicit target directory before previewing a move", async () => {
    const user = userEvent.setup();
    render(<AdminGameManager game={game} platformInstances={directories} candidates={[]} />);
    const preview = screen.getByRole("button", { name: "预览移动影响" });
    expect(preview).toBeDisabled();
    await user.selectOptions(screen.getByRole("combobox", { name: "目标游戏目录" }), "neo-geo");
    expect(preview).toBeEnabled();
  });
});
