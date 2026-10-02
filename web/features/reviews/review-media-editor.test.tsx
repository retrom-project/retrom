import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { ReviewMediaEditor } from "./review-media-editor";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("loads video only on selection and stops it when switching back with the keyboard", async () => {
  const pause = vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  const user = userEvent.setup();
  render(<ReviewMediaEditor cover={null} videoUrl="/source.webm" disabled={false} restoreLabel={null} onUpload={vi.fn()} onRestore={vi.fn()} />);
  expect(screen.getByRole("tab", { name: "封面" })).toHaveAttribute("aria-selected", "true");
  expect(screen.queryByLabelText("来源视频预览")).not.toBeInTheDocument();
  await user.click(screen.getByRole("tab", { name: "视频" }));
  const video = screen.getByLabelText("来源视频预览");
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
  render(<ReviewMediaEditor cover={null} videoUrl={null} disabled={false} restoreLabel="恢复来源封面" onUpload={upload} onRestore={restore} />);
  expect(screen.getByText("暂无封面")).toBeVisible();
  const file = new File(["cover"], "cover.png", { type: "image/png" });
  await user.upload(screen.getByLabelText("上传封面", { selector: "input" }), file);
  expect(upload).toHaveBeenCalledWith(file);
  await user.click(screen.getByRole("button", { name: "恢复来源封面" }));
  expect(restore).toHaveBeenCalledTimes(1);
  await user.click(screen.getByRole("tab", { name: "视频" }));
  expect(screen.getByText("暂无来源视频")).toBeVisible();
  expect(screen.queryByRole("button", { name: "恢复来源封面" })).not.toBeInTheDocument();
});
