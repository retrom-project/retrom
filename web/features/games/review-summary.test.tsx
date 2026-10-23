import { act, cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import type { Schema } from "@/lib/api/types";
import type * as ApiClient from "@/lib/api/client";
import { ReviewBiosStatus } from "./review-bios-status";
import { ReviewSummary } from "./review-summary";

const calls = vi.hoisted(() => ({ post: vi.fn(), replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: calls.replace }) }));
vi.mock("@/lib/api/client", async original => ({ ...await original<typeof ApiClient>(), api: { POST: calls.post } }));
afterEach(() => { cleanup(); vi.clearAllMocks(); });
const detail: Schema<"GameDetail"> = {
  game: { id: "game", version: 1, title: "测试游戏", platformId: "arcade", platformInstanceId: "dir", directoryName: "Arcade", titleLetter: "#", sortName: "", description: "", developer: "", publisher: "", genre: "", players: null, releaseYear: null, status: "pending_review", source: "server_import", contentHash: "hash", favorite: false, lastPlayedAtMs: null, tags: [], media: [], createdAtMs: 1, updatedAtMs: 1 },
  files: [], coreIds: ["fbneo", "mame2003"], defaultCoreId: "fbneo", runtimeConfig: { content: { kind: "ARCADE", entryFile: "game.zip" } }, saves: [], favoriteFolderIds: [],
};
const warning: Schema<"ReviewReadiness">["crcWarnings"][number] = { role: "ROM", file: "game.zip", member: "rom.bin", expectedCRC32: "11111111", actualCRC32: "22222222" };
function response(overrides: Partial<Schema<"ReviewReadiness">> = {}) {
  return { data: { items: [{ id: "game", version: 1, biosSatisfied: true, missingBios: [], missingRomMembers: [], crcWarnings: [warning], error: null, ...overrides }] }, response: new Response() };
}
function Screen({ coreId = "fbneo", game = detail, approve = vi.fn() }: { coreId?: string; game?: Schema<"GameDetail">; approve?: (action: "approve" | "discard", coreId?: string) => void }) {
  return <ReviewBiosStatus detail={game} coreId={coreId}>{check => <ReviewSummary detail={game} coreId={coreId} busy={false} onReview={approve} check={check} />}</ReviewBiosStatus>;
}

it("requires explicit CRC risk confirmation, defaults focus to cancel and allows approval without trial", async () => {
  calls.post.mockResolvedValue(response());
  const approve = vi.fn();
  render(<Screen approve={approve} />);
  fireEvent.click(await screen.findByRole("button", { name: "通过并发布" }));
  const dialog = screen.getByRole("alertdialog");
  expect(dialog).toHaveTextContent("测试运行不是强制条件");
  expect(within(dialog).getByRole("button", { name: "返回检查" })).toHaveFocus();
  expect(approve).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole("button", { name: "返回检查" }));
  expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "通过并发布" }));
  fireEvent.click(screen.getByRole("button", { name: "确认风险并发布" }));
  expect(approve).toHaveBeenCalledExactlyOnceWith("approve", "fbneo");
});

it("launches optional review play using the selected core without approving", async () => {
  calls.post.mockResolvedValueOnce(response()).mockResolvedValueOnce({ data: { id: "run" }, response: new Response() });
  const approve = vi.fn();
  render(<Screen coreId="mame2003" approve={approve} />);
  fireEvent.click(await screen.findByRole("button", { name: "通过并发布" }));
  fireEvent.click(screen.getByRole("button", { name: "测试运行" }));
  await vi.waitFor(() => expect(calls.replace).toHaveBeenCalled());
  expect(calls.post).toHaveBeenLastCalledWith("/api/v1/runs", { body: { gameId: "game", coreId: "mame2003", saveId: undefined, purpose: "review" } });
  expect(approve).not.toHaveBeenCalled();
});

it.each([
  { biosSatisfied: false, missingBios: [{ key: "bios", coreId: "fbneo", name: "bios.zip" }] },
  { missingRomMembers: [{ coreId: "fbneo", romSet: "game", role: "ROM" as const, member: "missing.bin", sizeBytes: 10, reason: "MISSING" as const }] },
  { error: { code: "REVIEW_READINESS_UNAVAILABLE", message: "检查失败" }, biosSatisfied: null },
  { version: 2 },
])("never offers risk override for a hard blocker: %j", async item => {
  calls.post.mockResolvedValue(response(item));
  render(<Screen />);
  expect(within(screen.getByRole("region", { name: "审核条目概览" })).getByRole("button", { name: "正在检查…" })).toBeDisabled();
  expect(await screen.findByRole("button", { name: "通过并发布" })).toBeDisabled();
  expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
});

it("publishes a clean result directly", async () => {
  calls.post.mockResolvedValue(response({ crcWarnings: [] }));
  const approve = vi.fn();
  render(<Screen approve={approve} />);
  fireEvent.click(await screen.findByRole("button", { name: "通过并发布" }));
  expect(approve).toHaveBeenCalledExactlyOnceWith("approve", "fbneo");
  expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
});

it.each(["core", "version", "file", "recheck"])("discards the open confirmation when %s changes", async change => {
  calls.post.mockResolvedValueOnce(response()).mockImplementation(() => new Promise(() => {}));
  const view = render(<Screen />);
  fireEvent.click(await screen.findByRole("button", { name: "通过并发布" }));
  expect(screen.getByRole("alertdialog")).toBeVisible();
  if (change === "core") { view.rerender(<Screen coreId="mame2003" />); }
  if (change === "version") { view.rerender(<Screen game={{ ...detail, game: { ...detail.game, version: 2 } }} />); }
  if (change === "file") { view.rerender(<Screen game={{ ...detail, files: [{ id: "file", logicalKey: "game.zip", role: "content", sizeBytes: 1, sha256: "new" }] }} />); }
  if (change === "recheck") { fireEvent.click(screen.getByRole("button", { name: "重新检查" })); }
  expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  expect(within(screen.getByRole("region", { name: "审核条目概览" })).getByRole("button", { name: "正在检查…" })).toBeDisabled();
});

it("ignores a late safe result after changing cores", async () => {
  let resolve!: (value: ReturnType<typeof response>) => void;
  calls.post.mockImplementationOnce(() => new Promise(done => { resolve = done; })).mockResolvedValueOnce(response({ biosSatisfied: false }));
  const view = render(<Screen />);
  view.rerender(<Screen coreId="mame2003" />);
  expect(await screen.findByRole("button", { name: "通过并发布" })).toBeDisabled();
  await act(async () => resolve(response({ crcWarnings: [] })));
  expect(screen.getByRole("button", { name: "通过并发布" })).toBeDisabled();
});
