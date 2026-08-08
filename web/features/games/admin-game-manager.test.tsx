import { cleanup, render, screen } from "@testing-library/react";
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
  afterEach(cleanup);

  it("renders the precise four-section workbench without the omitted section tags", () => {
    render(<AdminGameManager game={game} platformInstances={directories} candidates={[]} />);
    for (const heading of ["发布信息", "媒体", "游戏文件与运行环境", "管理操作", "从游戏库移除"]) {
      expect(screen.getByRole("heading", { name: heading })).toBeInTheDocument();
    }
    for (const omitted of ["媒体资源", "运行状态正常", "维护工具", "危险区域"]) {
      expect(screen.queryByText(omitted, { exact: true })).not.toBeInTheDocument();
    }
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
