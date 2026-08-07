import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { UploadPicker } from "./upload-picker";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
}));

afterEach(cleanup);

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

    expect(screen.getByRole("combobox", { name: "目标平台目录" })).toHaveValue("");
    expect(screen.getByRole("option", { name: "请选择目标平台目录" })).toBeDisabled();
    const submit = screen.getByRole("button", { name: "上传、验证并创建导入任务" });
    const file = new File(["rom"], "game.zip", { type: "application/zip" });
    await user.upload(screen.getByLabelText("选择导入文件"), file);
    expect(submit).toBeDisabled();

    await user.selectOptions(screen.getByRole("combobox", { name: "目标平台目录" }), "arcade");
    expect(submit).toBeEnabled();
  });
});
