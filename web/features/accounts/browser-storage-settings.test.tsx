import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { BrowserStorageSettings } from "./browser-storage-settings";
import { loadContentCacheManager, readSiteUsage } from "./browser-storage";

vi.mock("./browser-storage", async importOriginal => ({
  ...await importOriginal<Record<string, unknown>>(),
  loadContentCacheManager: vi.fn(), readSiteUsage: vi.fn(),
}));
const inspectContentCache = vi.fn(), clearContentCache = vi.fn();
beforeEach(() => {
  vi.mocked(readSiteUsage).mockResolvedValue(4_000_000);
  inspectContentCache.mockResolvedValue({ cachedBytes: 2_000_000, objectCount: 2, canClear: true });
  clearContentCache.mockResolvedValue({ reclaimedBytes: 2_000_000, removedObjects: 2, skippedObjects: 0, nextKey: null });
  vi.mocked(loadContentCacheManager).mockResolvedValue({ inspectContentCache, clearContentCache });
});
afterEach(() => { cleanup(); vi.resetAllMocks(); });

it("shows separate browser and content-cache values after client-side loading", async () => {
  render(<BrowserStorageSettings />);
  expect(await screen.findByText("约 4MB")).toBeVisible();
  expect(screen.getByText("2MB · 2 项")).toBeVisible();
  expect(screen.getByRole("button", { name: "清理资源缓存" })).toBeEnabled();
});

it("keeps an unavailable estimate distinct from an empty cache", async () => {
  vi.mocked(readSiteUsage).mockRejectedValue(new Error("denied"));
  inspectContentCache.mockResolvedValue({ cachedBytes: 0, objectCount: 0, canClear: true });
  render(<BrowserStorageSettings />);
  expect(await screen.findByText("0B · 0 项")).toBeVisible();
  expect(screen.getByText("暂不可用")).toBeVisible();
  expect(screen.queryByText("约 0B")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "清理资源缓存" })).toBeDisabled();
});

it("continues showing site usage when the management module cannot load", async () => {
  vi.mocked(loadContentCacheManager).mockRejectedValue(new Error("offline"));
  render(<BrowserStorageSettings />);
  expect(await screen.findByText("约 4MB")).toBeVisible();
  expect(screen.getByText(/暂时无法读取资源缓存/)).toBeVisible();
  expect(screen.getByRole("button", { name: "清理资源缓存" })).toBeDisabled();
});

it("requires confirmation, permits cancelling, and refreshes both values after actual clearing", async () => {
  render(<BrowserStorageSettings />);
  await screen.findByText("2MB · 2 项");
  fireEvent.click(screen.getByRole("button", { name: "清理资源缓存" }));
  let dialog = screen.getByRole("alertdialog");
  expect(within(dialog).getByText(/存档与未同步进度会保留/)).toBeVisible();
  fireEvent.click(within(dialog).getByRole("button", { name: "取消" }));
  expect(clearContentCache).not.toHaveBeenCalled();
  inspectContentCache.mockResolvedValue({ cachedBytes: 0, objectCount: 0, canClear: true });
  vi.mocked(readSiteUsage).mockResolvedValue(1_000_000);
  fireEvent.click(screen.getByRole("button", { name: "清理资源缓存" }));
  dialog = screen.getByRole("alertdialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "清理缓存" }));
  expect(await screen.findByText(/已清理 2 项资源缓存/)).toBeVisible();
  expect(await screen.findByText("约 1MB")).toBeVisible();
  expect(screen.getByText("0B · 0 项")).toBeVisible();
});

it("reports resources that remain occupied and leaves them available for a later attempt", async () => {
  clearContentCache.mockResolvedValue({ reclaimedBytes: 0, removedObjects: 0, skippedObjects: 2, nextKey: null });
  render(<BrowserStorageSettings />);
  await screen.findByText("2MB · 2 项");
  fireEvent.click(screen.getByRole("button", { name: "清理资源缓存" }));
  fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "清理缓存" }));
  expect(await screen.findByText(/另有 2 项正在使用或暂时无法清理/)).toBeVisible();
  await waitFor(() => expect(screen.getByRole("button", { name: "清理资源缓存" })).toBeEnabled());
});

it("reports partial failure and queries remaining storage instead of claiming success", async () => {
  clearContentCache.mockRejectedValue(new Error("delete failed"));
  render(<BrowserStorageSettings />);
  await screen.findByText("2MB · 2 项");
  fireEvent.click(screen.getByRole("button", { name: "清理资源缓存" }));
  fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "清理缓存" }));
  expect(await screen.findByText(/清理未完成/)).toBeVisible();
  await waitFor(() => expect(inspectContentCache).toHaveBeenCalledTimes(2));
});

it("aborts its storage operation when the settings page unmounts", async () => {
  let finish!: (value: number) => void;
  vi.mocked(readSiteUsage).mockReturnValue(new Promise(resolve => { finish = resolve; }));
  const { unmount } = render(<BrowserStorageSettings />);
  await waitFor(() => expect(inspectContentCache).toHaveBeenCalledOnce());
  const signal = inspectContentCache.mock.calls[0][0].signal as AbortSignal;
  unmount();
  expect(signal.aborted).toBe(true);
  await act(async () => { finish(0); });
});
