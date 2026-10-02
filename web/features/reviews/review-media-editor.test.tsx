import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { ReviewMediaEditor } from "./review-media-editor";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("loads video only on selection and stops it when switching back with the keyboard", async () => {
  const pause = vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  const user = userEvent.setup();
  render(<ReviewMediaEditor videoRestoreLabel={null} onUploadVideo={vi.fn()} onRestoreVideo={vi.fn()} cover={null} videoUrl="/source.webm" disabled={false} restoreLabel={null} onUpload={vi.fn()} onRestore={vi.fn()} />);
  expect(screen.getByRole("tab", { name: "封面" })).toHaveAttribute("aria-selected", "true");
  expect(screen.queryByLabelText("视频预览")).not.toBeInTheDocument();
  await user.click(screen.getByRole("tab", { name: "视频" }));
  const video = screen.getByLabelText("视频预览");
  expect(video).toHaveAttribute("src", "/source.webm");
  expect(video).not.toHaveAttribute("autoplay");
  expect(screen.getByRole("tabpanel", { name: "视频" })).toBeVisible();
  await user.keyboard("{Home}");
  expect(pause).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("tab", { name: "封面" })).toHaveFocus();
  expect(video).not.toBeInTheDocument();
  await user.keyboard("{End}");
  expect(screen.getByRole("tab", { name: "视频" })).toHaveFocus();
  await user.keyboard("{ArrowRight}");
  expect(screen.getByRole("tab", { name: "封面" })).toHaveFocus();
});

it("shows explicit empty states and preserves cover upload and restore controls", async () => {
  const upload = vi.fn();
  const restore = vi.fn();
  const user = userEvent.setup();
  render(<ReviewMediaEditor videoRestoreLabel={null} onUploadVideo={vi.fn()} onRestoreVideo={vi.fn()} cover={null} videoUrl={null} disabled={false} restoreLabel="恢复来源封面" onUpload={upload} onRestore={restore} />);
  expect(screen.getByText("暂无封面")).toBeVisible();
  const file = new File(["cover"], "cover.png", { type: "image/png" });
  await user.upload(screen.getByLabelText("上传封面", { selector: "input" }), file);
  expect(upload).toHaveBeenCalledWith(file);
  await user.click(screen.getByRole("button", { name: "恢复来源封面" }));
  expect(restore).toHaveBeenCalledTimes(1);
  await user.click(screen.getByRole("tab", { name: "视频" }));
  expect(screen.getByText("暂无视频")).toBeVisible();
  expect(screen.queryByRole("button", { name: "恢复来源封面" })).not.toBeInTheDocument();
});

it("uploads video from the empty tab, allows the same file again, and restores the source", async () => {
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  const upload = vi.fn();
  const restore = vi.fn();
  const user = userEvent.setup();
  const props = { cover: null, disabled: false, restoreLabel: null, onUpload: vi.fn(), onRestore: vi.fn(), onUploadVideo: upload, onRestoreVideo: restore };
  const view = render(<ReviewMediaEditor {...props} videoUrl={null} videoRestoreLabel={null} />);
  await user.click(screen.getByRole("tab", { name: "视频" }));
  expect(screen.getByRole("button", { name: "上传视频" })).toBeEnabled();
  const file = new File(["video"], "preview.webm", { type: "video/webm" });
  const input = screen.getByLabelText("上传视频", { selector: "input" });
  await user.upload(input, file);
  await user.upload(input, file);
  expect(upload).toHaveBeenCalledTimes(2);
  expect(upload).toHaveBeenLastCalledWith(file);
  view.rerender(<ReviewMediaEditor {...props} videoUrl="/uploaded.webm" videoRestoreLabel="恢复来源视频" />);
  expect(screen.getByRole("button", { name: "替换视频" })).toBeEnabled();
  expect(screen.getByLabelText("视频预览")).toHaveAttribute("src", "/uploaded.webm");
  await user.click(screen.getByRole("button", { name: "恢复来源视频" }));
  expect(restore).toHaveBeenCalledOnce();
  view.rerender(<ReviewMediaEditor {...props} disabled videoUrl="/uploaded.webm" videoRestoreLabel="恢复来源视频" />);
  expect(screen.getByRole("button", { name: "替换视频" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "恢复来源视频" })).toBeDisabled();
});
