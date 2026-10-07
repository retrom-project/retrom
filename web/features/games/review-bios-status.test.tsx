import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { api } from "@/lib/api/client";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { ReviewBiosStatus } from "./review-bios-status";

vi.mock("@/lib/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof ApiClient>();
  return { ...actual, api: { POST: vi.fn() } };
});
afterEach(() => { cleanup(); vi.clearAllMocks(); });
const post = api.POST<"/api/v1/admin/reviews/readiness", { body: { gameIds: string[] } }>;
const requirement: Schema<"ReviewMissingBIOS"> = { key: "runtime/core/os.rom", name: "os.rom", coreId: "core" };
const detail: Schema<"GameDetail"> = {
  game: { id: "game", version: 1, title: "游戏", platformId: "bbc", platformInstanceId: "directory", directoryName: "目录", description: "", developer: "", publisher: "", genre: "", players: null, releaseYear: null, status: "pending_review", source: "server_import", contentHash: "hash", favorite: false, lastPlayedAtMs: null, tags: [], media: [], createdAtMs: 1, updatedAtMs: 1 },
  files: [], coreIds: ["core"], defaultCoreId: "core", runtimeConfig: { content: { kind: "SINGLE_FILE", entryFile: "game.ssd" } }, saves: [], favoriteFolderIds: [],
};
function response(item: Partial<Schema<"ReviewReadiness">> = {}) {
  return { data: { items: [{ id: "game", version: 1, biosSatisfied: false, error: null, missingBios: [requirement], ...item }] }, response: new Response() };
}

it("shows missing runtime filenames and cores without duplicating file validation requirements", async () => {
  vi.mocked(post).mockResolvedValue(response());
  render(<ReviewBiosStatus detail={detail} />);
  expect(await screen.findByText("os.rom")).toBeVisible();
  expect(screen.getByText("核心：core · 必需")).toBeVisible();
  expect(screen.queryByText("文件校验要求")).not.toBeInTheDocument();
  expect(screen.queryByText(/要求大小|SHA-256|MD5/)).not.toBeInTheDocument();
  expect(screen.getByRole("link", { name: "管理运行依赖" })).toHaveAttribute("href", "/admin/bios");
  expect(post).toHaveBeenCalledExactlyOnceWith("/api/v1/admin/reviews/readiness", { body: { gameIds: ["game"] } });
});

it("keeps an inspection failure distinct from missing files and rechecks current installed facts", async () => {
  vi.mocked(post).mockResolvedValueOnce(response({ biosSatisfied: null, error: { code: "BIOS_REQUIREMENTS_UNAVAILABLE", message: "无法读取内容" }, missingBios: [] }))
    .mockResolvedValueOnce(response({ biosSatisfied: true, missingBios: [] }));
  render(<ReviewBiosStatus detail={detail} />);
  expect(await screen.findByRole("alert")).toHaveTextContent("无法检查 BIOS：无法读取内容");
  expect(screen.queryByText(/缺少 .*项必需 BIOS/)).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "重新检查" }));
  expect(await screen.findByText(/必需 BIOS 已满足/)).toBeVisible();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.queryByText("os.rom")).not.toBeInTheDocument();
});

it("drops stale missing-file results when the saved game version changes", async () => {
  let resolve!: (value: Awaited<ReturnType<typeof post>>) => void;
  vi.mocked(post).mockImplementationOnce(() => new Promise((done) => { resolve = done; }))
    .mockResolvedValueOnce(response({ version: 2, biosSatisfied: true, missingBios: [] }));
  const view = render(<ReviewBiosStatus detail={detail} />);
  view.rerender(<ReviewBiosStatus detail={{ ...detail, game: { ...detail.game, version: 2 } }} />);
  expect(await screen.findByText(/必需 BIOS 已满足/)).toBeVisible();
  await act(async () => { resolve(response()); });
  expect(screen.queryByText("os.rom")).not.toBeInTheDocument();
});
