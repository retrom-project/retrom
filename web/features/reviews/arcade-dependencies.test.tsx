import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ArcadeDependencyCard } from "./arcade-dependencies";
import { type ArcadeDependencies } from "./arcade-dependency-tree";

const dependencies: ArcadeDependencies = {
  machine: "a", status: "BLOCKED", compatibilityCode: "LAUNCH_PARENT_MISSING", activeAttachment: null,
  nodes: [
    { kind: "PARENT", machine: "b", requiredBy: "a", depth: 1, expectedLogicalName: "b.zip", state: "MISSING", requiredEntryCount: 12, canAttach: true, attachment: null },
    { kind: "BIOS_OR_BASE", machine: "bios-x", requiredBy: "b", depth: 2, expectedLogicalName: "bios-x.zip", state: "MISSING", requiredEntryCount: 2, canAttach: false, managementUrl: "/admin/bios", attachment: null },
  ],
};

describe("ArcadeDependencyCard", () => {
  afterEach(cleanup);

  it("renders server order and only exposes valid actions", () => {
    render(<ArcadeDependencyCard value={dependencies} disabled={false} progress="" onAttach={vi.fn()} onRetry={vi.fn()} />);

    const rows = screen.getAllByRole("listitem");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("b.zip");
    expect(rows[0]).toHaveTextContent("由 a.zip 需要");
    expect(within(rows[0]).getByRole("button", { name: "补充 Parent ROM" })).toBeEnabled();
    expect(rows[1]).toHaveTextContent("bios-x.zip");
    expect(within(rows[1]).getByRole("link", { name: "前往 BIOS 文件" })).toHaveAttribute("href", "/admin/bios");
    expect(within(rows[1]).queryByRole("button", { name: /Parent ROM/ })).not.toBeInTheDocument();
  });

  it("accepts a differently named ZIP and keeps the dialog open until a file is selected", async () => {
    const onAttach = vi.fn().mockResolvedValue(true);
    const user = userEvent.setup();
    render(<ArcadeDependencyCard value={dependencies} disabled={false} progress="" onAttach={onAttach} onRetry={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "补充 Parent ROM" }));
    const dialog = screen.getByRole("alertdialog", { name: "补充 b.zip" });
    const picker = within(dialog).getByLabelText("选择一个 ZIP");
    await user.click(within(dialog).getByRole("button", { name: "开始上传并校验" }));
    expect(within(dialog).getByRole("alert")).toHaveTextContent("请选择一个 ZIP");
    expect(onAttach).not.toHaveBeenCalled();

    await user.upload(picker, new File(["parent"], "anything.zip", { type: "application/zip" }));
    await user.click(within(dialog).getByRole("button", { name: "开始上传并校验" }));
    expect(onAttach).toHaveBeenCalledWith(expect.objectContaining({ machine: "b" }), expect.objectContaining({ name: "anything.zip" }));
  });

  it("offers retry without asking the user to upload the bytes again", () => {
    const attachment = { attachmentId: "attachment-1", machine: "b", expectedLogicalName: "b.zip", originalFilename: "anything.zip", state: "FAILED_RETRYABLE" as const, errorCode: "REVIEW_PARENT_VALIDATION_UNAVAILABLE", jobId: "job-1" };
    render(<ArcadeDependencyCard value={{ ...dependencies, nodes: [{ ...dependencies.nodes[0], attachment }] }} disabled={false} progress="" onAttach={vi.fn()} onRetry={vi.fn()} />);

    expect(screen.getByRole("button", { name: "重试校验" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "补充 Parent ROM" })).not.toBeInTheDocument();
  });

  it("does not report dependency work when the dependency tree is empty", () => {
    render(<ArcadeDependencyCard value={{ ...dependencies, compatibilityCode: "UNSUPPORTED_MERGED_ROMSET", nodes: [] }} disabled={false} progress="" onAttach={vi.fn()} onRetry={vi.fn()} />);

    expect(screen.getByText("无额外依赖")).toHaveClass("status", "info");
    expect(screen.queryByText("需要处理")).not.toBeInTheDocument();
    expect(screen.getByText("当前没有额外 Parent 或 BIOS/Base 依赖。")).toBeVisible();
  });
});
