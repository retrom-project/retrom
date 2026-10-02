import { readFileSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";
import { evidencePath, noPageOverflow } from "./acceptance-support";
import { createVideoReview, deleteVideoFixture } from "./review-video-support";

async function selectedVideo(page: Page, itemId: string) {
  const response = await page.request.get(`/api/v1/admin/reviews/${itemId}`);
  expect(response.ok()).toBe(true);
  return (await response.json() as { selectedAssets: { videoUploadedAssetId: string | null } }).selectedAssets.videoUploadedAssetId;
}

async function uploadVideo(page: Page) {
  const player = page.getByLabel("视频预览");
  const before = await player.count() ? await player.getAttribute("src") : null;
  const chooser = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: /^(上传|替换)视频$/ }).click();
  await (await chooser).setFiles("../testdata/public-roms/gba-smoke/emulationstation-smoke-video.webm");
  await expect(player).toBeVisible();
  if (before) {await expect(player).not.toHaveAttribute("src", before);}
  await expect(page.locator(".autosave-state")).toHaveText("已实时保存");
}

test("ACC-MEDIA-001 review uploads, replaces, retains and publishes the selected video", async ({ page }, testInfo) => {
  test.setTimeout(90_000);
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const login = await page.request.post("/api/v1/auth/login", { data: { username: "test", password: "test" }, headers: { Origin: origin } });
  expect(login.ok()).toBe(true);
  const { csrfToken } = await login.json() as { csrfToken: string };
  const title = `Review video ${testInfo.project.name}`;
  await deleteVideoFixture(page, title, csrfToken);
  const itemId = await createVideoReview(page, testInfo.project.name, csrfToken);
  await page.goto(`/admin/reviews/${itemId}`);
  await page.getByRole("textbox", { name: "标题", exact: true }).fill(title);
  await expect(page.locator(".autosave-state")).toHaveText("已实时保存");
  await page.getByRole("tab", { name: "视频", exact: true }).click();
  await expect(page.getByText("暂无视频")).toBeVisible();
  await uploadVideo(page);
  const first = await selectedVideo(page, itemId);
  expect(first).not.toBeNull();
  await uploadVideo(page);
  await expect.poll(() => selectedVideo(page, itemId)).not.toBe(first);
  const second = await selectedVideo(page, itemId);
  await page.reload();
  await page.getByRole("tab", { name: "视频", exact: true }).click();
  const player = page.getByLabel("视频预览");
  await expect(player).toHaveAttribute("src", `/api/v1/admin/review-assets/${second}`);
  await expect.poll(() => player.evaluate((element: HTMLVideoElement) => element.readyState)).toBeGreaterThanOrEqual(2);
  await player.evaluate((element: HTMLVideoElement) => element.play());
  await expect.poll(() => player.evaluate((element: HTMLVideoElement) => element.currentTime)).toBeGreaterThan(0);
  const before = await page.locator(".review-workflow-metadata").boundingBox();
  await page.getByRole("tab", { name: "封面", exact: true }).click();
  await expect(player).toHaveCount(0);
  await expect(page.getByRole("button", { name: "上传封面" })).toBeEnabled();
  expect((await page.locator(".review-workflow-metadata").boundingBox())?.height).toBeCloseTo(before!.height, 0);
  await page.getByRole("tab", { name: "视频", exact: true }).click();
  await expect(player).toBeVisible();
  await expect(page.getByRole("tab", { name: "视频", exact: true })).toHaveAttribute("aria-selected", "true");
  await noPageOverflow(page);
  await page.screenshot({ path: evidencePath(testInfo, "review-uploaded-video.png"), fullPage: true });
  const range = await page.request.get(`/api/v1/admin/review-assets/${second}`, { headers: { Range: "bytes=0-9" } });
  expect(range.status()).toBe(206);
  expect((await range.body()).length).toBe(10);
  const approve = page.waitForResponse((response) => response.url().endsWith(`/reviews/${itemId}/approve`));
  await page.getByRole("button", { name: "通过并发布", exact: true }).click();
  const result = await approve;
  expect(result.ok()).toBe(true);
  await expect(page).not.toHaveURL(new RegExp(`/admin/reviews/${itemId}`));
  const published = await page.request.get(`/api/v1/admin/games?q=${encodeURIComponent(title)}`);
  const matches = (await published.json() as { items: Array<{ gameId: string; title: string }> }).items.filter((game) => game.title === title);
  expect(matches).toHaveLength(1);
  const { gameId } = matches[0];
  const game = await page.request.get(`/api/v1/games/${gameId}`);
  const detail = await game.json() as { videoUrl: string };
  const video = await page.request.get(detail.videoUrl);
  expect(video.ok()).toBe(true);
  expect(await video.body()).toEqual(readFileSync("../testdata/public-roms/gba-smoke/emulationstation-smoke-video.webm"));
  expect((await page.request.get(`/api/v1/admin/review-assets/${second}`)).status()).toBe(404);
  expect((await page.request.get(`/api/v1/admin/review-assets/${first}`)).status()).toBe(404);
  const adminResponse = await page.request.get(`/api/v1/admin/games/${gameId}`);
  const admin = await adminResponse.json() as { title: string; deleteImpact: { impactDigest: string } };
  const deleted = await page.request.delete(`/api/v1/admin/games/${gameId}`, { headers: { Origin: origin, "X-Retrom-Csrf": csrfToken, "If-Match": adminResponse.headers().etag, "Idempotency-Key": crypto.randomUUID() }, data: { confirmTitle: admin.title, impactDigest: admin.deleteImpact.impactDigest } });
  expect(deleted.ok()).toBe(true);
});
