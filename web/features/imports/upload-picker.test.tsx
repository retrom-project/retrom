import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type * as DirectoryAccess from "@/lib/directory-access";
import type * as UploadModule from "@/lib/upload";
import { UploadPicker } from "./upload-picker";

const directoryAccess = vi.hoisted(() => ({ directoryPickerAvailable: vi.fn(), pickDirectory: vi.fn() }));
const upload = vi.hoisted(() => ({ uploadFiles: vi.fn() }));
const router = vi.hoisted(() => ({ push: vi.fn(), refresh: vi.fn() }));

vi.mock("@/lib/directory-access", async (loadOriginal) => {
  const original = await loadOriginal<typeof DirectoryAccess>();
  return { ...original, directoryPickerAvailable: directoryAccess.directoryPickerAvailable, pickDirectory: directoryAccess.pickDirectory };
});

vi.mock("@/lib/upload", async (loadOriginal) => {
  const original = await loadOriginal<typeof UploadModule>();
  return { ...original, uploadFiles: upload.uploadFiles };
});

vi.mock("next/navigation", () => ({
  useRouter: () => router,
}));

beforeEach(() => {
  directoryAccess.directoryPickerAvailable.mockReset().mockReturnValue(true);
  directoryAccess.pickDirectory.mockReset();
  upload.uploadFiles.mockReset().mockResolvedValue({ uploadId: "rpg-upload", uploadFileIds: ["rpg-file"] });
  router.push.mockReset();
  router.refresh.mockReset();
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

async function useDirectory(files: Array<{ file: File; relativePath: string }>, name = "game") {
  const user = userEvent.setup();
  directoryAccess.pickDirectory.mockResolvedValue({ files, name });
  await user.click(screen.getByRole("button", { name: "选择目录" }));
  await user.click(screen.getByRole("button", { name: "浏览本机目录" }));
  await user.click(await screen.findByRole("button", { name: "使用此目录" }));
}

describe("UploadPicker", () => {
  it("groups the file and directory actions in the centered dropzone control row", () => {
    render(<UploadPicker directories={[{ id: "arcade", name: "街机游戏", platformName: "Arcade", coreName: "FinalBurn Neo" }]} />);

    const actions = screen.getByRole("button", { name: "选择文件" }).closest(".dropzone-actions");
    expect(actions).not.toBeNull();
    expect(actions).toContainElement(screen.getByRole("button", { name: "选择目录" }));
  });

  it("confirms a selected directory in an application dialog before using it", async () => {
    const user = userEvent.setup();
    render(<UploadPicker directories={[]} />);

    const trigger = screen.getByRole("button", { name: "选择目录" });
    await user.click(trigger);
    const dialog = screen.getByRole("dialog", { name: "选择游戏目录" });
    expect(dialog).toBeVisible();
    expect(screen.getByRole("button", { name: "使用此目录" })).toBeDisabled();

    const rom = new File(["rom"], "game.gba");
    directoryAccess.pickDirectory.mockResolvedValue({ files: [{ file: rom, relativePath: "GBA/game.gba" }], name: "GBA" });
    await user.click(screen.getByRole("button", { name: "浏览本机目录" }));
    expect(within(dialog).getByRole("heading", { name: "GBA" })).toBeVisible();
    expect(within(dialog).getByText("1 个文件 · 3 B")).toBeVisible();
    expect(within(dialog).queryByText("目录已读取")).not.toBeInTheDocument();
    expect(screen.queryByText(/文件相对路径会完整保留，上传前仍可重新选择/)).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "使用此目录" }));
    expect(screen.queryByRole("dialog", { name: "选择游戏目录" })).not.toBeInTheDocument();
    expect(screen.getByText(/文件相对路径会完整保留，上传前仍可重新选择/)).toBeVisible();
  });

  it("falls back to the browser directory input when the handle API is unavailable", async () => {
    const user = userEvent.setup();
    directoryAccess.directoryPickerAvailable.mockReturnValue(false);
    render(<UploadPicker directories={[]} />);

    await user.click(screen.getByRole("button", { name: "选择目录" }));
    await user.click(screen.getByRole("button", { name: "浏览本机目录" }));
    const rom = new File(["rom"], "game.gba");
    Object.defineProperty(rom, "webkitRelativePath", { value: "GBA/game.gba" });
    await user.upload(screen.getByLabelText("选择导入目录"), rom);

    expect(await screen.findByRole("heading", { name: "GBA" })).toBeVisible();
    expect(directoryAccess.pickDirectory).not.toHaveBeenCalled();
  });

  it("requires the user to choose a platform directory explicitly", async () => {
    const user = userEvent.setup();
    render(<UploadPicker directories={[{ id: "arcade", name: "街机游戏", platformName: "Arcade", coreName: "FinalBurn Neo" }]} />);

    const file = new File(["rom"], "game.zip", { type: "application/zip" });
    await user.upload(screen.getByLabelText("选择导入文件"), file);
    await user.click(screen.getByRole("button", { name: "下一步" }));

    expect(screen.getByRole("combobox", { name: "目标游戏目录" })).toHaveValue("");
    expect(screen.getByRole("option", { name: "请选择目标游戏目录" })).toBeDisabled();
    const submit = screen.getByRole("button", { name: "开始上传并验证" });
    expect(submit).toBeDisabled();

    await user.selectOptions(screen.getByRole("combobox", { name: "目标游戏目录" }), "arcade");
    expect(submit).toBeEnabled();
  });

  it("uses the explicit RPG Maker core directory and RPG project upload purpose", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ importJobId: "rpg-import" }), { status: 202, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<UploadPicker directories={[{
      id: "rpg-mv", name: "RPG Maker MV", platformName: "RPG Maker", coreName: "RPG Maker MV",
      importCapabilities: { contentModes: ["RPG_MAKER_PROJECT_V1"], multiDisc: null },
    }]} />);

    const project = new File(["project"], "game.zip", { type: "application/zip" });
    await user.upload(screen.getByLabelText("选择导入文件"), project);
    await user.click(screen.getByRole("button", { name: "下一步" }));
    await user.selectOptions(screen.getByRole("combobox", { name: "目标游戏目录" }), "rpg-mv");
    expect(screen.getByText("RPG Maker 项目")).toBeVisible();
    expect(screen.getByText(/服务端会识别项目版本并选择底层核心/)).toBeVisible();
    expect(screen.getByLabelText("元信息来源")).toHaveValue("不刮削（RPG Maker 项目）");
    expect(screen.getByLabelText("元信息来源")).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "上一步" }));
    expect(screen.getByRole("heading", { name: "将 RPG Maker 项目归档或目录拖到这里" })).toBeVisible();
    expect(screen.getByRole("button", { name: "选择项目归档" })).toBeVisible();
    expect(screen.getByRole("button", { name: "选择项目目录" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "下一步" }));

    await user.click(screen.getByRole("button", { name: "上传并验证 RPG Maker 项目" }));
    expect(upload.uploadFiles).toHaveBeenCalledWith(expect.any(Array), expect.any(Function), "RPG_MAKER_PROJECT");
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/imports", expect.objectContaining({
      body: JSON.stringify({ uploadId: "rpg-upload", targetPlatformInstanceId: "rpg-mv", metadataProvider: "NONE", contentMode: "RPG_MAKER_PROJECT_V1", tagIds: [] }),
    }));
  });

  it("uses the ONS project upload purpose and disables metadata scraping", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ importJobId: "ons-import" }), { status: 202, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<UploadPicker directories={[{
      id: "ons", name: "ONS 游戏", platformName: "ONS", coreName: "ONScripterYuri",
      importCapabilities: { contentModes: ["ONS_PROJECT_V1"], multiDisc: null },
    }]} />);

    const project = new File(["project"], "game.7z", { type: "application/x-7z-compressed" });
    await user.upload(screen.getByLabelText("选择导入文件"), project);
    await user.click(screen.getByRole("button", { name: "下一步" }));
    await user.selectOptions(screen.getByRole("combobox", { name: "目标游戏目录" }), "ons");
    expect(screen.getByText("ONS 项目")).toBeVisible();
    expect(screen.getByText(/审核时需要先成功试运行一次/)).toBeVisible();
    expect(screen.getByLabelText("元信息来源")).toHaveValue("不刮削（ONS 项目）");
    expect(screen.getByLabelText("元信息来源")).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "上传并试运行 ONS 项目" }));
    expect(upload.uploadFiles).toHaveBeenCalledWith(expect.any(Array), expect.any(Function), "ONS_PROJECT");
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/imports", expect.objectContaining({
      body: JSON.stringify({ uploadId: "rpg-upload", targetPlatformInstanceId: "ons", metadataProvider: "NONE", contentMode: "ONS_PROJECT_V1", tagIds: [] }),
    }));
  });

  it("uses the KiriKiri project upload purpose and only requires a successful trial run", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ importJobId: "kirikiri-import" }), { status: 202, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<UploadPicker directories={[{
      id: "kirikiri", name: "KiriKiri 游戏", platformName: "KiriKiri", coreName: "KiriKiri2",
      importCapabilities: { contentModes: ["KIRIKIRI_PROJECT_V1"], multiDisc: null },
    }]} />);

    const project = new File(["project"], "game.zip", { type: "application/zip" });
    await user.upload(screen.getByLabelText("选择导入文件"), project);
    await user.click(screen.getByRole("button", { name: "下一步" }));
    await user.selectOptions(screen.getByRole("combobox", { name: "目标游戏目录" }), "kirikiri");
    expect(screen.getByText("KiriKiri 项目")).toBeVisible();
    expect(screen.getByText(/审核时需要先成功试运行一次/)).toBeVisible();
    expect(screen.getByLabelText("元信息来源")).toHaveValue("不刮削（KiriKiri 项目）");
    expect(screen.getByLabelText("元信息来源")).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "上传并试运行 KiriKiri 项目" }));
    expect(upload.uploadFiles).toHaveBeenCalledWith(expect.any(Array), expect.any(Function), "KIRIKIRI_PROJECT");
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/imports", expect.objectContaining({
      body: JSON.stringify({ uploadId: "rpg-upload", targetPlatformInstanceId: "kirikiri", metadataProvider: "NONE", contentMode: "KIRIKIRI_PROJECT_V1", tagIds: [] }),
    }));
  });

  it("uses the GameMaker project upload purpose and exposes the trial runtime", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ importJobId: "butterscotch-import" }), { status: 202, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<UploadPicker directories={[{
      id: "butterscotch", name: "GameMaker 游戏", platformName: "GameMaker", coreName: "Butterscotch",
      importCapabilities: { contentModes: ["BUTTERSCOTCH_PROJECT_V1"], multiDisc: null },
    }]} />);

    await user.upload(screen.getByLabelText("选择导入文件"), new File(["FORM"], "game.zip"));
    await user.click(screen.getByRole("button", { name: "下一步" }));
    await user.selectOptions(screen.getByRole("combobox", { name: "目标游戏目录" }), "butterscotch");
    expect(screen.getByText("GameMaker 项目")).toBeVisible();
    expect(screen.getByText(/当前原型支持带 data.win/)).toBeVisible();
    expect(screen.getByLabelText("元信息来源")).toHaveValue("不刮削（GameMaker 项目）");

    await user.click(screen.getByRole("button", { name: "上传并试运行 GameMaker 项目" }));
    expect(upload.uploadFiles).toHaveBeenCalledWith(expect.any(Array), expect.any(Function), "BUTTERSCOTCH_PROJECT");
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/imports", expect.objectContaining({
      body: JSON.stringify({ uploadId: "rpg-upload", targetPlatformInstanceId: "butterscotch", metadataProvider: "NONE", contentMode: "BUTTERSCOTCH_PROJECT_V1", tagIds: [] }),
    }));
  });

  it("uses the TyranoScript project upload purpose and trial workflow", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({importJobId: "tyrano-import"}), {
      status: 202, headers: {"Content-Type": "application/json"},
    }));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<UploadPicker directories={[{
      id: "tyranoscript", name: "TyranoScript 游戏", platformName: "TyranoScript", coreName: "TyranoScript",
      importCapabilities: {contentModes: ["TYRANOSCRIPT_PROJECT_V1"], multiDisc: null},
    }]} />);

    await user.upload(screen.getByLabelText("选择导入文件"), new File(["project"], "game.zip"));
    await user.click(screen.getByRole("button", {name: "下一步"}));
    await user.selectOptions(screen.getByRole("combobox", {name: "目标游戏目录"}), "tyranoscript");
    expect(screen.getByText("TyranoScript 项目")).toBeVisible();
    expect(screen.getByText(/审核时需要先成功试运行一次/)).toBeVisible();
    expect(screen.getByLabelText("元信息来源")).toHaveValue("不刮削（TyranoScript 项目）");

    await user.click(screen.getByRole("button", {name: "上传并试运行 TyranoScript 项目"}));
    expect(upload.uploadFiles).toHaveBeenCalledWith(expect.any(Array), expect.any(Function), "TYRANOSCRIPT_PROJECT");
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/imports", expect.objectContaining({
      body: JSON.stringify({
        uploadId: "rpg-upload", targetPlatformInstanceId: "tyranoscript", metadataProvider: "NONE",
        contentMode: "TYRANOSCRIPT_PROJECT_V1", tagIds: [],
      }),
    }));
  });

  it("opens task progress as soon as the server accepts background preparation", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ importJobId: "queued-import", jobId: "group-job", state: "QUEUED", itemCount: 0 }),
      { status: 202, headers: { "Content-Type": "application/json" } },
    ));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<UploadPicker directories={[{
      id: "ons", name: "ONS 游戏", platformName: "ONS", coreName: "ONScripterYuri",
      importCapabilities: { contentModes: ["ONS_PROJECT_V1"], multiDisc: null },
    }]} />);

    await user.upload(screen.getByLabelText("选择导入文件"), new File(["project"], "game.zip"));
    await user.click(screen.getByRole("button", { name: "下一步" }));
    await user.selectOptions(screen.getByRole("combobox", { name: "目标游戏目录" }), "ons");
    await user.click(screen.getByRole("button", { name: "上传并试运行 ONS 项目" }));

    expect(router.push).toHaveBeenCalledWith("/admin/imports/tasks");
    expect(router.refresh).toHaveBeenCalledOnce();
  });

  it("preflights a directory and blocks multi-disc mode on an unsupported target", async () => {
    const user = userEvent.setup();
    render(<UploadPicker directories={[
      { id: "gba", name: "GBA 游戏", platformName: "Game Boy Advance", coreName: "mGBA", importCapabilities: { contentModes: ["STANDARD"], multiDisc: null } },
      { id: "saturn", name: "Saturn 游戏", platformName: "Sega Saturn", coreName: "Yabause", importCapabilities: { contentModes: ["STANDARD", "MULTI_DISC_M3U_V1"], multiDisc: { maxDiscs: 8, maxTotalBytes: 1024 } } },
    ]} />);
    const playlist = new File(["one.chd\ntwo.chd\n"], "game.m3u");
    const firstDisc = new File(["MComprHDone"], "one.chd");
    await useDirectory([
      { file: playlist, relativePath: "game/game.m3u" },
      { file: firstDisc, relativePath: "game/one.chd" },
    ]);
    expect(await screen.findByText("可以继续，审核会阻断")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "下一步" }));
    expect(screen.queryByRole("checkbox", { name: /多盘游戏/ })).not.toBeInTheDocument();

    await user.selectOptions(screen.getByRole("combobox", { name: "目标游戏目录" }), "gba");
    expect(screen.getByRole("alert")).toHaveTextContent("当前平台核心不支持多盘游戏");
    expect(screen.getByRole("button", { name: "开始上传并验证" })).toBeEnabled();

    await user.selectOptions(screen.getByRole("combobox", { name: "目标游戏目录" }), "saturn");
    expect(await screen.findByRole("checkbox", { name: /多盘游戏/ })).toBeChecked();
    expect(screen.getByRole("button", { name: "继续上传并在审核补齐" })).toBeEnabled();

    await user.click(screen.getByRole("checkbox", { name: /多盘游戏/ }));
    expect(screen.getByRole("checkbox", { name: /多盘游戏/ })).not.toBeChecked();
    expect(screen.getByRole("button", { name: "开始上传并验证" })).toBeEnabled();
  });

  it("lists recursive groups, expands the first incomplete group and counts only processable games", async () => {
    render(<UploadPicker directories={[]} />);
    const files = [
      new File(["a.chd\nb.chd\n"], "game.m3u"),
      new File(["MComprHDa"], "a.chd"),
      new File(["MComprHDb"], "b.chd"),
      new File(["x.chd\ny.chd\n"], "game.m3u"),
      new File(["MComprHDx"], "x.chd"),
      new File(["one.chd\ntwo.chd\n"], "one.m3u"),
      new File(["one.chd\ntwo.chd\n"], "two.m3u"),
    ];
    const paths = ["complete/game.m3u", "complete/a.chd", "complete/b.chd", "blocked/game.m3u", "blocked/x.chd", "invalid/one.m3u", "invalid/two.m3u"];
    await useDirectory(files.map((file, index) => ({ file, relativePath: paths[index] })));

    expect(await screen.findByText("发现多盘游戏")).toBeVisible();
    expect(screen.getByText("2", { selector: ".multi-disc-preflight-summary strong" })).toBeVisible();
    expect(screen.getByRole("button", { name: /blocked.*game\.m3u.*1 \/ 2 张.*缺少光盘/ })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("button", { name: /complete.*game\.m3u.*2 \/ 2 张.*目录完整/ })).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByRole("button", { name: /invalid.*多个 M3U.*不可处理/ })).toHaveAttribute("aria-expanded", "false");
  });

  it("reuses rejected server files and submits a new platform without uploading bytes", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ importJobId: "replacement-import" }), { status: 202, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    render(<UploadPicker directories={[
      { id: "gba", name: "GBA 游戏", platformName: "Game Boy Advance", coreName: "mGBA" },
      { id: "psp", name: "PSP 游戏", platformName: "PlayStation Portable", coreName: "PPSSPP" },
    ]} activeTags={[{ tagId: "tag-handheld", name: "掌机" }]} reconfigureSource={{
      importJobId: "source-import",
      state: "PARTIAL_FAILURE",
      payloadState: "RETAINED",
      payloadReleaseJobId: null,
      metadataProvider: "HASHEOUS",
      targetPlatformInstance: { id: "gba", name: "GBA 游戏" },
      counts: { total: 0, reviewPending: 0, failed: 0, rejectedFiles: 1, unresolvedRejectedFiles: 1, alreadyImportedItems: 0, alreadyImportedFiles: 0 },
      version: 3,
      createdAtMs: 1,
      updatedAtMs: 2,
      configSnapshot: { tags: [{ tagId: "tag-handheld", name: "掌机" }] },
      fileOutcomes: [{ uploadFileId: "file-1", name: "game.iso", sizeBytes: 1024, disposition: "REJECTED", reasonCode: "UNSUPPORTED_CONTENT_FORMAT", resolution: null }],
    }} />);

    expect(screen.getByText("复用服务器中已上传的内容")).toBeVisible();
    expect(screen.getByText(/不会再次上传文件内容/)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "重新选择平台目录" }));
    expect(screen.getByRole("button", { name: "移除标签“掌机”" })).toBeVisible();
    await user.selectOptions(screen.getByRole("combobox", { name: "目标游戏目录" }), "psp");
    await user.click(screen.getByRole("button", { name: "按新配置重新识别" }));

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/imports/source-import/reconfigure", expect.objectContaining({
      method: "POST",
      headers: expect.objectContaining({ "If-Match": '"v3"' }),
      body: JSON.stringify({ targetPlatformInstanceId: "psp", metadataProvider: "HASHEOUS", tagIds: ["tag-handheld"] }),
    }));
    expect(await screen.findByText("导入任务已创建")).toBeVisible();
  });
});
