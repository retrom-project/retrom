import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type * as UploadModule from "@/lib/upload";
import { AdminGameManager, type AdminGame, type PlatformInstanceOption } from "./admin-game-manager";

vi.mock("next/navigation", () => ({ useRouter: () => ({ refresh: vi.fn() }) }));
const upload = vi.hoisted(() => ({ uploadFiles: vi.fn(), waitForJob: vi.fn() }));
vi.mock("@/lib/upload", async (importOriginal) => {
  const original = await importOriginal<typeof UploadModule>();
  return { ...original, uploadFiles: upload.uploadFiles, waitForJob: upload.waitForJob };
});

const game: AdminGame = {
  gameId: "game-1",
  status: "PUBLISHED",
  payloadState: "RETAINED",
  payloadReleaseJobId: null,
  title: "1943",
  description: "Battle of Midway",
  developer: "Capcom",
  publisher: "Capcom",
  genre: "Shoot 'em up",
  players: 2,
  releaseYear: 1987,
  platformId: "arcade",
  platformInstance: { id: "fbneo-games", name: "FBNeo 游戏" },
  contentKind: "SINGLE",
  files: [{ role: "CONTENT", logicalName: "1943.zip", sortOrder: 0, sizeBytes: 4, sha256: "a".repeat(64), md5: "b".repeat(32), sha1: "c".repeat(40), crc32: "12345678", mediaType: "application/zip" }],
  version: 3,
  createdAtMs: 100,
  updatedAtMs: 200,
  generatedAtMs: 500,
  deleteImpact: {
    impactDigest: "d".repeat(64), registeredBytes: "5242880",
    fileCount: 4, saveStateCount: 44, assetCount: 2,
    contentFileCount: 1, activeLaunchCount: 0, sourceKinds: ["USER_UPLOAD"],
  },
  assets: [],
  variants: [{ id: "variant-1", coreId: "fbneo", coreName: "FinalBurn Neo", providerId: "libretro", targetId: "fbneo", datVersionId: null, status: "READY", compatibilityCode: "READY", version: 1, createdAtMs: 180, updatedAtMs: 180 }],
};

const directories: PlatformInstanceOption[] = [
  { id: "fbneo-games", platformId: "arcade", platformName: "Arcade", name: "FBNeo 游戏", defaultCoreId: "fbneo", defaultCoreName: "FinalBurn Neo", enabled: true, importCapabilities: { contentModes: ["STANDARD"], multiDisc: null } },
  { id: "neo-geo", platformId: "arcade", platformName: "Arcade", name: "Neo Geo", defaultCoreId: "fbneo", defaultCoreName: "FinalBurn Neo", enabled: true, importCapabilities: { contentModes: ["STANDARD"], multiDisc: null } },
];

describe("AdminGameManager", () => {
  beforeEach(() => {
    upload.uploadFiles.mockReset();
    upload.waitForJob.mockReset().mockResolvedValue(undefined);
  });
  afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

  it("renders the precise four-section workbench without the omitted section tags", () => {
    const { container } = render(<AdminGameManager game={game} platformInstances={directories} candidates={[]} />);
    for (const heading of ["发布信息", "媒体", "游戏文件", "管理操作", "危险操作"]) {
      expect(screen.getByRole("heading", { name: heading })).toBeInTheDocument();
    }
    for (const omitted of ["媒体资源", "运行状态正常", "维护工具", "危险区域"]) {
      expect(screen.queryByText(omitted, { exact: true })).not.toBeInTheDocument();
    }
    expect(screen.queryByRole("navigation", { name: "游戏管理详情分区" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "永久删除游戏" })).toBeVisible();
    expect(container.querySelector(".admin-game-cover-frame")).not.toBeNull();
    expect(screen.getByRole("tab", { name: "封面" })).toBeVisible();
    expect(screen.getByRole("tab", { name: "视频" })).toBeVisible();
    expect(screen.queryByRole("heading", { name: "背景图" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "游戏截图" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存发布信息" })).toBeDisabled();
  });

  it("shows a deleted game as deleted instead of runnable", () => {
    render(<AdminGameManager game={{ ...game, status: "DELETED" }} platformInstances={directories} candidates={[]} />);

    expect(screen.getAllByText("已删除").length).toBeGreaterThan(0);
    expect(screen.queryByText("可以运行")).not.toBeInTheDocument();
  });

  it("confirms permanent deletion without a title field and submits the loaded title internally", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ payloadState: "RELEASING" }), {
      status: 202,
      headers: { "Content-Type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<AdminGameManager game={game} platformInstances={directories} candidates={[]} />);

    await user.click(screen.getByRole("button", { name: "永久删除游戏" }));
    const dialog = screen.getByRole("alertdialog", { name: "永久删除“1943”？" });
    expect(within(dialog).queryByRole("textbox")).not.toBeInTheDocument();
    expect(within(dialog).queryByText("输入完整游戏标题确认")).not.toBeInTheDocument();
    const confirm = within(dialog).getByRole("button", { name: "永久删除游戏" });
    expect(confirm).toBeEnabled();
    await user.click(confirm);

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/games/game-1", expect.objectContaining({
      method: "DELETE",
      body: JSON.stringify({ confirmTitle: "1943", impactDigest: game.deleteImpact.impactDigest }),
    })));
  });

  it("enables metadata save only for an unsaved change and disables it after success", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      gameId: game.gameId,
      version: 4,
      updatedAtMs: 600,
    }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    render(<AdminGameManager game={game} platformInstances={directories} candidates={[]} />);
    const save = screen.getByRole("button", { name: "保存发布信息" });
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

  it("replaces the complete game tag set under the current game version", async () => {
    const actionTag = { tagId: "tag-action", name: "动作" };
    const coopTag = { tagId: "tag-coop", name: "双人合作" };
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      gameId: game.gameId, version: 4, tags: [actionTag, coopTag],
    }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<AdminGameManager game={{ ...game, tags: [actionTag] }} platformInstances={directories} candidates={[]} activeTags={[actionTag, coopTag]} />);

    const save = screen.getByRole("button", { name: "更新标签" });
    expect(save).toBeDisabled();
    await user.type(screen.getByRole("combobox", { name: "标签" }), "合作");
    await user.keyboard("{Enter}");
    expect(save).toBeEnabled();
    await user.click(save);

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/games/game-1/tags", expect.objectContaining({
      method: "PUT",
      headers: expect.objectContaining({ "If-Match": '"v3"' }),
      body: JSON.stringify({ tagIds: ["tag-action", "tag-coop"] }),
    })));
    await waitFor(() => expect(save).toBeDisabled());
    expect(document.querySelector(".admin-game-hero-copy")).not.toHaveTextContent("双人合作");
    expect(screen.getAllByText("双人合作", { exact: true })).toHaveLength(1);
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

  it("previews video only on demand and removes the current video", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204, headers: { ETag: '"v4"' } }));
    vi.stubGlobal("fetch", fetchMock);
    render(<AdminGameManager game={{ ...game, assets: [
      { assetId: "video-1", kind: "VIDEO", ordinal: 0, widthPx: null, heightPx: null, mediaType: "video/mp4", url: "/content/assets/video-1" },
      { assetId: "background-1", kind: "BACKGROUND", ordinal: 0, widthPx: 1280, heightPx: 720, mediaType: "image/jpeg", url: "/content/assets/background-1" },
      { assetId: "screenshot-1", kind: "SCREENSHOT", ordinal: 0, widthPx: 640, heightPx: 480, mediaType: "image/png", url: "/content/assets/screenshot-1" },
    ] }} platformInstances={directories} candidates={[]} />);

    expect(screen.queryByLabelText("1943 管理视频预览")).not.toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "视频" }));
    const video = screen.getByLabelText("1943 管理视频预览");
    expect(video).toHaveAttribute("preload", "metadata");
    expect(video).not.toHaveAttribute("autoplay");
    expect(screen.queryByRole("heading", { name: "背景图" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "游戏截图" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "封面" }));
    expect(screen.queryByLabelText("1943 管理视频预览")).not.toBeInTheDocument();
    await user.keyboard("{End}");
    expect(screen.getByRole("tab", { name: "视频" })).toHaveFocus();
    await user.click(screen.getByRole("button", { name: "移除视频" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/games/game-1/assets/VIDEO", expect.objectContaining({ method: "DELETE" })));
  });

});
