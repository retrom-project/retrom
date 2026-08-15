import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { UploadPicker } from "./upload-picker";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
}));

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("UploadPicker", () => {
  it("groups the file and directory actions in the centered dropzone control row", () => {
    render(<UploadPicker directories={[{ id: "arcade", name: "街机游戏", platformName: "Arcade", coreName: "FinalBurn Neo" }]} />);

    const actions = screen.getByRole("button", { name: "选择文件" }).closest(".dropzone-actions");
    expect(actions).not.toBeNull();
    expect(actions).toContainElement(screen.getByRole("button", { name: "选择目录" }));
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

  it("preflights a directory and blocks multi-disc mode on an unsupported target", async () => {
    const user = userEvent.setup();
    render(<UploadPicker directories={[
      { id: "gba", name: "GBA 游戏", platformName: "Game Boy Advance", coreName: "mGBA", importCapabilities: { contentModes: ["STANDARD"], multiDisc: null } },
      { id: "saturn", name: "Saturn 游戏", platformName: "Sega Saturn", coreName: "Yabause", importCapabilities: { contentModes: ["STANDARD", "MULTI_DISC_M3U_V1"], multiDisc: { maxDiscs: 8, maxTotalBytes: 1024 } } },
    ]} />);
    const playlist = new File(["one.chd\ntwo.chd\n"], "game.m3u");
    const firstDisc = new File(["MComprHDone"], "one.chd");
    Object.defineProperty(playlist, "webkitRelativePath", { value: "game/game.m3u" });
    Object.defineProperty(firstDisc, "webkitRelativePath", { value: "game/one.chd" });

    await user.upload(screen.getByLabelText("选择导入目录"), [playlist, firstDisc]);
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
    const user = userEvent.setup();
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
    for (const [file, path] of files.map((file, index) => [file, ["complete/game.m3u", "complete/a.chd", "complete/b.chd", "blocked/game.m3u", "blocked/x.chd", "invalid/one.m3u", "invalid/two.m3u"][index]] as const)) {
      Object.defineProperty(file, "webkitRelativePath", { value: path });
    }
    await user.upload(screen.getByLabelText("选择导入目录"), files);

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
