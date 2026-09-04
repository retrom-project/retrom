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
  currentContentRevisionId: "content-1",
  currentMetadataRevisionId: "metadata-1",
  version: 3,
  createdAtMs: 100,
  updatedAtMs: 200,
  generatedAtMs: 500,
  deleteImpact: {
    impactDigest: "d".repeat(64), registeredBytes: "5242880", exclusiveBytes: "4194304",
    sharedBytes: "1048576", blobCount: 4, saveStateCount: 44, assetCount: 2,
    contentFileCount: 1, activeLaunchCount: 0, activeNetplayCount: 1,
    reviewEventCount: 2, sourceKinds: ["USER_UPLOAD"],
  },
  metadataRevisions: [{ id: "metadata-1", sourceKind: "MANUAL", sourceRefId: null, current: true, createdAtMs: 200 }],
  assets: [],
  contentRevisions: [{ id: "content-1", sourceKind: "IMPORT", sourceRefId: "upload-1", contentKind: "SINGLE", current: true, createdAtMs: 150, files: [{ role: "CONTENT", logicalName: "1943.zip", sortOrder: 0, sizeBytes: 4, sha256: "a".repeat(64) }] }],
  variants: [{ id: "variant-1", coreId: "fbneo", coreName: "FinalBurn Neo", currentRevisionId: "variant-revision-1", version: 1, revisions: [{ id: "variant-revision-1", contentRevisionId: "content-1", datVersionId: null, status: "READY", compatibilityCode: "READY", current: true, createdAtMs: 180 }] }],
};

const directories: PlatformInstanceOption[] = [
  { id: "fbneo-games", platformId: "arcade", platformName: "Arcade", name: "FBNeo 游戏", defaultCoreId: "fbneo", defaultCoreName: "FinalBurn Neo", enabled: true, importCapabilities: { contentModes: ["STANDARD"], multiDisc: null } },
  { id: "neo-geo", platformId: "arcade", platformName: "Arcade", name: "Neo Geo", defaultCoreId: "fbneo", defaultCoreName: "FinalBurn Neo", enabled: true, importCapabilities: { contentModes: ["STANDARD"], multiDisc: null } },
];

function directoryFile(path: string, contents: string) {
  const file = new File([contents], path.split("/").at(-1) ?? path);
  Object.defineProperty(file, "webkitRelativePath", { value: path });
  return file;
}

describe("AdminGameManager", () => {
  beforeEach(() => {
    upload.uploadFiles.mockReset();
    upload.waitForJob.mockReset().mockResolvedValue(undefined);
  });
  afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

  it("renders the precise four-section workbench without the omitted section tags", () => {
    const { container } = render(<AdminGameManager game={game} platformInstances={directories} candidates={[]} />);
    for (const heading of ["发布信息", "媒体", "游戏文件与运行环境", "管理操作", "危险操作"]) {
      expect(screen.getByRole("heading", { name: heading })).toBeInTheDocument();
    }
    for (const omitted of ["媒体资源", "运行状态正常", "维护工具", "危险区域"]) {
      expect(screen.queryByText(omitted, { exact: true })).not.toBeInTheDocument();
    }
    expect(screen.queryByRole("navigation", { name: "游戏管理详情分区" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "永久删除游戏" })).toBeVisible();
    expect(container.querySelector(".admin-game-cover-slot > .admin-game-cover-frame")).not.toBeNull();
    expect(screen.getByRole("heading", { name: "封面" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "视频预览" })).toBeVisible();
    expect(screen.queryByRole("heading", { name: "背景图" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "游戏截图" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存新版本" })).toBeDisabled();
  });

  it("bounds project file details instead of hydrating a multi-megabyte path list", () => {
    const files = Array.from({ length: 7 }, (_, index) => ({
      role: "PROJECT_FILE",
      logicalName: `data/scenario/file-${index}.ks`,
      sortOrder: index,
      sizeBytes: 4,
      sha256: String(index).repeat(64),
    }));
    render(<AdminGameManager game={{
      ...game,
      contentRevisions: [{ ...game.contentRevisions[0]!, files }],
    }} platformInstances={directories} candidates={[]} />);

    expect(screen.getByText(/file-0\.ks、.*file-4\.ks 等 7 个文件/)).toBeInTheDocument();
    expect(screen.queryByText(/file-5\.ks/)).not.toBeInTheDocument();
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

  it("hides multi-disc replacement when the current directory lacks the capability", async () => {
    const user = userEvent.setup();
    render(<AdminGameManager game={game} platformInstances={directories} candidates={[]} />);

    await user.click(screen.getByRole("button", { name: "替换游戏文件" }));
    const dialog = screen.getByRole("alertdialog", { name: "替换游戏内容" });
    expect(within(dialog).queryByRole("checkbox", { name: /多盘游戏/ })).not.toBeInTheDocument();
    expect(within(dialog).getByText(/当前游戏目录只允许替换普通内容/)).toBeVisible();
	expect(within(dialog).getByText(/44 份存档会永久失效/)).toBeVisible();
  });

  it("previews video only on demand and removes it through an immutable media revision", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204, headers: { ETag: '"v4"' } }));
    vi.stubGlobal("fetch", fetchMock);
    render(<AdminGameManager game={{ ...game, assets: [
      { assetId: "video-1", kind: "VIDEO", ordinal: 0, widthPx: null, heightPx: null, mediaType: "video/mp4", url: "/content/assets/video-1" },
      { assetId: "background-1", kind: "BACKGROUND", ordinal: 0, widthPx: 1280, heightPx: 720, mediaType: "image/jpeg", url: "/content/assets/background-1" },
      { assetId: "screenshot-1", kind: "SCREENSHOT", ordinal: 0, widthPx: 640, heightPx: 480, mediaType: "image/png", url: "/content/assets/screenshot-1" },
    ] }} platformInstances={directories} candidates={[]} />);

    const video = screen.getByLabelText("1943 管理视频预览");
    expect(video).toHaveAttribute("preload", "metadata");
    expect(video).not.toHaveAttribute("autoplay");
    expect(screen.queryByRole("heading", { name: "背景图" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "游戏截图" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "移除" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/games/game-1/assets/VIDEO", expect.objectContaining({ method: "DELETE" })));
  });

  it("preflights one complete multi-disc directory before creating a replacement job", async () => {
    const capableDirectories: PlatformInstanceOption[] = directories.map((directory) => directory.id === "fbneo-games" ? {
      ...directory,
      importCapabilities: { contentModes: ["STANDARD", "MULTI_DISC"], multiDisc: { maxDiscs: 8, maxTotalBytes: 1024 } },
    } : directory);
    upload.uploadFiles.mockResolvedValue({ uploadId: "upload-multi", uploadFileIds: ["playlist", "one", "two"] });
    const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(() => Promise.resolve(new Response(JSON.stringify({ jobId: "job-content" }), { status: 202, headers: { "Content-Type": "application/json" } })));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<AdminGameManager game={game} platformInstances={capableDirectories} candidates={[]} />);

    await user.click(screen.getByRole("button", { name: "替换游戏文件" }));
    const dialog = screen.getByRole("alertdialog", { name: "替换游戏内容" });
    await user.click(within(dialog).getByRole("checkbox", { name: /多盘游戏/ }));
    const files = [
      directoryFile("game/game.m3u", "one.chd\ntwo.chd\n"),
      directoryFile("game/one.chd", "MComprHDone"),
      directoryFile("game/two.chd", "MComprHDtwo"),
    ];
    await user.upload(within(dialog).getByLabelText("选择一份完整多盘目录"), files);

    expect(await within(dialog).findByText("目录完整，可以上传")).toBeVisible();
    const confirm = within(dialog).getByRole("button", { name: "上传并替换内容" });
    expect(confirm).toBeEnabled();
    await user.click(confirm);

    await waitFor(() => expect(upload.uploadFiles).toHaveBeenCalledWith(files, expect.any(Function)));
    const request = fetchMock.mock.calls.find(([url]) => String(url).endsWith("/content-revisions"));
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({ uploadId: "upload-multi", contentMode: "MULTI_DISC" });
    expect(upload.waitForJob).toHaveBeenCalledWith("job-content", expect.any(Function));
    await waitFor(() => expect(screen.queryByRole("alertdialog", { name: "替换游戏内容" })).not.toBeInTheDocument());
  });

  it("does not allow a missing-disc directory to replace published content", async () => {
    const capableDirectories: PlatformInstanceOption[] = directories.map((directory) => directory.id === "fbneo-games" ? {
      ...directory,
      importCapabilities: { contentModes: ["STANDARD", "MULTI_DISC"], multiDisc: { maxDiscs: 8, maxTotalBytes: 1024 } },
    } : directory);
    const user = userEvent.setup();
    render(<AdminGameManager game={game} platformInstances={capableDirectories} candidates={[]} />);

    await user.click(screen.getByRole("button", { name: "替换游戏文件" }));
    const dialog = screen.getByRole("alertdialog", { name: "替换游戏内容" });
    await user.click(within(dialog).getByRole("checkbox", { name: /多盘游戏/ }));
    await user.upload(within(dialog).getByLabelText("选择一份完整多盘目录"), [
      directoryFile("game/game.m3u", "one.chd\ntwo.chd\n"),
      directoryFile("game/one.chd", "MComprHDone"),
    ]);

    expect(await within(dialog).findByText("目录不完整，不能替换")).toBeVisible();
    expect(within(dialog).getByText("不能替换当前内容。")).toBeVisible();
    expect(within(dialog).getByRole("button", { name: "上传并替换内容" })).toBeDisabled();
    expect(upload.uploadFiles).not.toHaveBeenCalled();
  });

  it("uploads an RPG Maker replacement as one project and explains a generation mismatch", async () => {
    const rpgDirectory: PlatformInstanceOption = {
      id: "rpgmaker-games", platformId: "rpgmaker", platformName: "RPG Maker",
      name: "RPG Maker 游戏", defaultCoreId: "rpgmaker", defaultCoreName: "RPG Maker",
      enabled: true, importCapabilities: { contentModes: ["RPG_MAKER_PROJECT"], multiDisc: null },
    };
    const rpgGame: AdminGame = {
      ...game,
      platformId: "rpgmaker",
      platformInstance: { id: rpgDirectory.id, name: rpgDirectory.name },
      currentContentRevisionId: "rpg-content",
      contentRevisions: [{
        id: "rpg-content", sourceKind: "IMPORT", sourceRefId: "rpg-import",
        contentKind: "RPG_MAKER_PROJECT", current: true, createdAtMs: 150,
        files: [{ role: "PROJECT_FILE", logicalName: "RPG_RT.ldb", sortOrder: 0, sizeBytes: 4, sha256: "b".repeat(64) }],
      }],
      variants: [{
        id: "rpg-variant", coreId: "rpgmaker_2000", coreName: "RPG Maker 2000",
        currentRevisionId: "rpg-revision", version: 1,
        revisions: [{
          id: "rpg-revision", contentRevisionId: "rpg-content",
          datVersionId: null, status: "READY", compatibilityCode: "READY", current: true, createdAtMs: 180,
        }],
      }],
    };
    const files = [
      directoryFile("replacement/RPG_RT.ldb", "database"),
      directoryFile("replacement/RPG_RT.lmt", "map tree"),
    ];
    upload.uploadFiles.mockResolvedValue({ uploadId: "rpg-upload", uploadFileIds: ["ldb", "lmt"] });
    upload.waitForJob.mockRejectedValue(new Error("RPG_REPLACEMENT_GENERATION_MISMATCH"));
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ jobId: "rpg-job" }), {
      status: 202, headers: { "Content-Type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<AdminGameManager game={rpgGame} platformInstances={[rpgDirectory]} candidates={[]} />);

    await user.click(screen.getByRole("button", { name: "替换游戏文件" }));
    const dialog = screen.getByRole("alertdialog", { name: "替换游戏内容" });
    expect(screen.getByText("RPG Maker 项目")).toBeVisible();
    expect(within(dialog).getByText(/服务端会重新识别世代/)).toBeVisible();
    await user.upload(within(dialog).getByLabelText("选择同世代 RPG Maker 项目目录"), files);
    await user.click(within(dialog).getByRole("button", { name: "上传并替换内容" }));

    await waitFor(() => expect(upload.uploadFiles).toHaveBeenCalledWith(files, expect.any(Function)));
    const request = fetchMock.mock.calls.find(([url]) => String(url).endsWith("/content-revisions"));
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({
      uploadId: "rpg-upload", contentMode: "RPG_MAKER_PROJECT",
    });
    expect(await screen.findByText("替换项目属于另一个 RPG Maker 世代，当前游戏内容与存档未变更。")).toBeVisible();
  });

	it("explains that an identical ROM was rejected without reporting a replacement", async () => {
	  upload.uploadFiles.mockResolvedValue({ uploadId: "upload-same", uploadFileIds: ["same-file"] });
	  upload.waitForJob.mockRejectedValue(new Error("GAME_CONTENT_UNCHANGED"));
	  const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ jobId: "job-same" }), {
		status: 202, headers: { "Content-Type": "application/json" },
	  }));
	  vi.stubGlobal("fetch", fetchMock);
	  const user = userEvent.setup();
	  render(<AdminGameManager game={game} platformInstances={directories} candidates={[]} />);

	  await user.click(screen.getByRole("button", { name: "替换游戏文件" }));
	  const dialog = screen.getByRole("alertdialog", { name: "替换游戏内容" });
	  await user.upload(within(dialog).getByLabelText("选择新的游戏文件"), new File(["same"], "1943.zip"));
	  await user.click(within(dialog).getByRole("button", { name: "上传并替换内容" }));

	  expect(await screen.findByText("所选游戏文件与当前内容相同，未执行替换。")).toBeVisible();
	  expect(screen.getByRole("alertdialog", { name: "替换游戏内容" })).toBeVisible();
	});
});
