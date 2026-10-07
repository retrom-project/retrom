import {
  act,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { render } from "@/components/toast-test-utils";
import { expect, it, vi } from "vitest";
import type { Schema } from "@/lib/api/types";
import { api } from "@/lib/api/client";
import { loadDirectories, loadTags } from "@/features/library/api";
import { GameEditor } from "./game-editor";

vi.mock("@/features/library/api", () => ({
  loadDirectories: vi.fn(),
  loadTags: vi.fn(),
}));
vi.mock("@/lib/api/client", () => ({
  api: { PATCH: vi.fn() },
  result: (response: { data: Schema<"GameDetail"> }) => response.data,
}));
vi.mock("./runtime-config-editor", () => ({ RuntimeConfigEditor: () => null }));

const detail: Schema<"GameDetail"> = {
  game: {
    id: "game",
    platformInstanceId: "original-directory",
    directoryName: "原目录",
    platformId: "nes",
    title: "游戏",
    description: "",
    developer: "",
    publisher: "",
    genre: "",
    players: null,
    releaseYear: null,
    status: "pending_review",
    source: "server_import",
    version: 1,
    contentHash: "hash",
    tags: [],
    media: [],
    favorite: false, lastPlayedAtMs: null,
    createdAtMs: 1,
    updatedAtMs: 1,
  },
  files: [],
  coreIds: ["fceumm"],
  defaultCoreId: "fceumm",
  saves: [],
  favoriteFolderIds: [],
  runtimeConfig: { content: { kind: "SINGLE_FILE", entryFile: "game.nes" } },
};

it("keeps the original directory when asynchronous options arrive and preserves player ranges", async () => {
  let resolveDirectories!: (value: Schema<"DirectoryList">) => void;
  vi.mocked(loadDirectories).mockReturnValue(
    new Promise((resolve) => {
      resolveDirectories = resolve;
    }),
  );
  vi.mocked(loadTags).mockResolvedValue({ items: [] });
  vi.mocked(api.PATCH).mockResolvedValue({
    data: detail,
    response: new Response(),
  } as Awaited<ReturnType<typeof api.PATCH>>);
  const onSaved = vi.fn();
  render(<GameEditor detail={detail} mode="review" onSaved={onSaved} />);
  expect(screen.getByLabelText("游戏目录")).toHaveValue("original-directory");
  await act(async () =>
    resolveDirectories({
      items: [
        {
          id: "first-directory",
          platformId: "nes",
          name: "第一个目录",
          slug: "first",
          description: "",
          coreIds: ["fceumm"],
          defaultCoreId: "fceumm",
          enabled: true,
          version: 1,
          gameCount: 0,
        },
        {
          id: "original-directory",
          platformId: "nes",
          name: "原目录",
          slug: "original",
          description: "",
          coreIds: ["fceumm"],
          defaultCoreId: "fceumm",
          enabled: true,
          version: 1,
          gameCount: 1,
        },
      ],
    }),
  );
  expect(screen.getByLabelText("游戏目录")).toHaveValue("original-directory");
  fireEvent.change(screen.getByLabelText("玩家人数"), {
    target: { value: "1-2" },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存发布信息" }));
  await waitFor(() =>
    expect(api.PATCH).toHaveBeenCalledWith(
      "/api/v1/admin/reviews/{gameId}",
      expect.objectContaining({
        body: expect.objectContaining({
          platformInstanceId: "original-directory",
          players: "1-2",
          releaseYear: null,
        }),
      }),
    ),
  );
  expect(await screen.findByRole("status")).toHaveTextContent("游戏资料与运行配置已保存");
  expect(onSaved).toHaveBeenCalledOnce();
  vi.mocked(api.PATCH).mockRejectedValueOnce(new Error("保存失败，请稍后重试。"));
  fireEvent.click(screen.getByRole("button", { name: "保存发布信息" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("保存失败，请稍后重试。");
  expect(screen.getAllByText("保存失败，请稍后重试。")).toHaveLength(1);
  expect(screen.getByLabelText("玩家人数")).toHaveValue("1-2");
  expect(onSaved).toHaveBeenCalledOnce();
});
