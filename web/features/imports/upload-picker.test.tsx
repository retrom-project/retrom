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
    const firstDisc = new File(["disc"], "one.chd");
    Object.defineProperty(playlist, "webkitRelativePath", { value: "game/game.m3u" });
    Object.defineProperty(firstDisc, "webkitRelativePath", { value: "game/one.chd" });

    await user.upload(screen.getByLabelText("选择导入目录"), [playlist, firstDisc]);
    expect(await screen.findByText("目录缺少 1 张光盘")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "下一步" }));
    expect(screen.getByRole("radio", { name: "多盘 M3U + CHD" })).toBeChecked();

    await user.selectOptions(screen.getByRole("combobox", { name: "目标游戏目录" }), "gba");
    expect(screen.getByText(/不支持多盘导入/)).toBeVisible();
    expect(screen.getByRole("button", { name: "继续上传并在审核补齐" })).toBeDisabled();

    await user.selectOptions(screen.getByRole("combobox", { name: "目标游戏目录" }), "saturn");
    expect(screen.getByRole("button", { name: "继续上传并在审核补齐" })).toBeEnabled();
  });

  it("reuses rejected server files and submits a new platform without uploading bytes", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ importJobId: "replacement-import" }), { status: 202, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    render(<UploadPicker directories={[
      { id: "gba", name: "GBA 游戏", platformName: "Game Boy Advance", coreName: "mGBA" },
      { id: "psp", name: "PSP 游戏", platformName: "PlayStation Portable", coreName: "PPSSPP" },
    ]} reconfigureSource={{
      importJobId: "source-import",
      state: "PARTIAL_FAILURE",
      metadataProvider: "HASHEOUS",
      targetPlatformInstance: { id: "gba", name: "GBA 游戏" },
      counts: { total: 0, reviewPending: 0, failed: 0, rejectedFiles: 1, unresolvedRejectedFiles: 1, alreadyImportedItems: 0, alreadyImportedFiles: 0 },
      version: 3,
      createdAtMs: 1,
      updatedAtMs: 2,
      fileOutcomes: [{ uploadFileId: "file-1", name: "game.iso", sizeBytes: 1024, disposition: "REJECTED", reasonCode: "UNSUPPORTED_CONTENT_FORMAT", resolution: null }],
    }} />);

    expect(screen.getByText("复用服务器中已上传的内容")).toBeVisible();
    expect(screen.getByText(/不会再次上传文件内容/)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "重新选择平台目录" }));
    await user.selectOptions(screen.getByRole("combobox", { name: "目标游戏目录" }), "psp");
    await user.click(screen.getByRole("button", { name: "按新配置重新识别" }));

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/imports/source-import/reconfigure", expect.objectContaining({
      method: "POST",
      headers: expect.objectContaining({ "If-Match": '"v3"' }),
      body: JSON.stringify({ targetPlatformInstanceId: "psp", metadataProvider: "HASHEOUS" }),
    }));
    expect(await screen.findByText("导入任务已创建")).toBeVisible();
  });
});
