import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useState } from "react";
import { render } from "@/components/toast-test-utils";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { AdminGameDetail } from "./admin-game-detail";

const calls = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), upload: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn(), replace: vi.fn() }) }));
vi.mock("@/lib/api/client", async (original) => ({ ...await original<typeof ApiClient>(), api: { GET: calls.get, POST: calls.post }, upload: calls.upload }));
vi.mock("@/features/library/api", () => ({ loadDirectories: async () => ({ items: [] }), loadTags: async () => ({ items: [] }) }));
afterEach(() => { cleanup(); vi.clearAllMocks(); });
const original: Schema<"GameDetail"> = {
  game: { id: "child", version: 1, title: "Child", platformId: "arcade", platformInstanceId: "directory", directoryName: "Arcade", description: "", developer: "", publisher: "", genre: "", players: null, releaseYear: null, status: "pending_review", source: "server_import", contentHash: "hash", favorite: false, lastPlayedAtMs: null, tags: [], media: [], createdAtMs: 1, updatedAtMs: 1 },
  files: [{ id: "entry", logicalKey: "1941j.zip", role: "content", sizeBytes: 1, sha256: "hash" }],
  coreIds: ["fbneo", "mame2003_plus"], defaultCoreId: "fbneo",
  runtimeConfig: { content: { kind: "ARCADE", entryFile: "1941j.zip" }, cores: { fbneo: { options: { existing: true } }, mame2003_plus: {} } },
  saves: [], favoriteFolderIds: [],
};
const updated: Schema<"GameDetail"> = { ...original, game: { ...original.game, version: 2 },
  files: [...original.files, { id: "parent", logicalKey: "1941.zip", role: "parent", sizeBytes: 1, sha256: "parent-hash" }],
  runtimeConfig: { ...original.runtimeConfig, cores: { ...original.runtimeConfig.cores, mame2003_plus: { parentFiles: ["1941.zip"] } } },
};
function Editor() {
  const [detail, setDetail] = useState(original);
  return <AdminGameDetail key={detail.game.id} detail={detail} mode="review" busy={false} onReview={vi.fn()} onChange={() => setDetail(updated)} />;
}
it("keeps the second selected core through upload's version reload and shows that core's new parent binding", async () => {
  let uploaded = false;
  calls.get.mockImplementation(async (path: string, init?: { params: { query: { coreId: string } } }) => ({
    response: new Response(), data: path === "/api/v1/runtime/catalog" ? {
      platforms: [], cores: original.coreIds.map((id) => ({ id, name: id, platformIds: ["arcade"], fingerprint: null })), providers: [],
      bindings: original.coreIds.map((coreId) => ({ coreId, providerId: "provider", targetId: coreId, platformIds: ["arcade"], contentKinds: ["ARCADE"] })),
    } : { coreId: init?.params.query.coreId, parentFiles: uploaded && init?.params.query.coreId === "mame2003_plus" ? ["1941.zip"] : [], missingParents: uploaded && init?.params.query.coreId === "mame2003_plus" ? [] : ["1941.zip"] },
  }));
  calls.post.mockImplementation(async () => ({ data: { items: [{ id: "child", version: uploaded ? 2 : 1, biosSatisfied: true, error: null, missingBios: [] }] }, response: new Response() }));
  calls.upload.mockImplementation(async () => { uploaded = true; return updated; });
  render(<Editor />);
  const core = await screen.findByRole("combobox", { name: "运行核心" });
  expect(core).toHaveValue("fbneo");
  fireEvent.change(core, { target: { value: "mame2003_plus" } });
  await screen.findByText("1941.zip", { selector: "li" });
  fireEvent.change(screen.getByLabelText("上传父包", { selector: "input" }), { target: { files: [new File(["zip"], "1941.zip")] } });
  await waitFor(() => expect(screen.getByRole("listbox", { name: "Parent 文件" })).toHaveValue(["1941.zip"]));
  expect(screen.getByRole("combobox", { name: "运行核心" })).toHaveValue("mame2003_plus");
  expect(await screen.findByText("已保存配置未发现缺失父包。")).toBeVisible();
  expect(calls.upload.mock.calls[0][1].get("coreId")).toBe("mame2003_plus");
  expect(calls.get).toHaveBeenLastCalledWith("/api/v1/admin/games/{gameId}/runtime-options/arcade", { params: { path: { gameId: "child" }, query: { coreId: "mame2003_plus" } } });
});
