import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createRef } from "react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FavoriteActions, type FavoriteActionsHandle } from "./favorite-actions";
import { FolderEditDialog, FolderNameDialog, FolderPickerDialog } from "./folder-dialogs";

const auth = vi.hoisted(() => ({ fetch: vi.fn() }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ authenticatedFetch: auth.fetch }) }));

const gameId = "01980000-0000-7000-8000-000000000001";
const folderId = "01980000-0000-7000-8000-000000000002";
const createdFolderId = "01980000-0000-7000-8000-000000000003";

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), { status, headers: { "Content-Type": "application/json" } });
}

function page() {
  return {
    generatedAtMs: 1000,
    summary: { favoriteCount: 1, uncategorizedCount: 1, folderCount: 1 },
    folders: [{ folderId, name: "想玩", version: 1, visibleGameCount: 0, createdAtMs: 1000, updatedAtMs: 1000 }],
    platforms: [], totalCount: 0, items: [], nextCursor: null,
  };
}

describe("FavoriteActions", () => {
  beforeEach(() => { auth.fetch.mockReset(); });
  afterEach(cleanup);

  it("favorites a game and exactly replaces multiple folder membership", async () => {
    auth.fetch.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path === `/api/v1/favorites/${gameId}`) return json({ gameId, favoritedAtMs: 1200, folderIds: [] });
      if (path.startsWith("/api/v1/favorites?")) return json(page());
      if (path.endsWith("/folders") && init?.method === "PUT") return json({ gameId, favoritedAtMs: 1200, folderIds: [folderId] });
      throw new Error(`unexpected ${init?.method} ${path}`);
    });
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<FavoriteActions gameId={gameId} title="Metroid" initialFavorite={null} onChange={onChange} />);

    await user.click(screen.getByRole("button", { name: "收藏“Metroid”" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "取消收藏“Metroid”" })).toHaveAttribute("aria-pressed", "true"));
    expect(onChange).toHaveBeenLastCalledWith({ favoritedAtMs: 1200, folderIds: [] });

    await user.click(screen.getByRole("button", { name: "加入收藏夹" }));
    expect(await screen.findByRole("dialog", { name: "管理“Metroid”的收藏夹" })).not.toHaveAttribute("aria-modal");
    await user.click(screen.getByRole("checkbox", { name: /想玩/ }));
    await user.click(screen.getByRole("button", { name: "完成" }));

    await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({ favoritedAtMs: 1200, folderIds: [folderId] }));
    const replace = auth.fetch.mock.calls.find(([input, init]) => String(input).endsWith("/folders") && init?.method === "PUT");
    expect(JSON.parse(String(replace?.[1]?.body))).toEqual({ folderIds: [folderId] });
  });

  it("confirms unfavorite and restores the server snapshot from the two-second undo", async () => {
    auth.fetch.mockImplementation(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith("/unfavorite")) return json({ items: [{ gameId, folderIds: [folderId] }] });
      if (path.endsWith("/restore")) return json({ restoredGameIds: [gameId], skippedGameIds: [], skippedFolderIds: [] });
      if (path === `/api/v1/favorites/${gameId}`) return json({ gameId, favoritedAtMs: 1200, folderIds: [folderId] });
      throw new Error(`unexpected ${path}`);
    });
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<FavoriteActions gameId={gameId} title="Metroid" initialFavorite={{ favoritedAtMs: 1000, folderIds: [folderId] }} onChange={onChange} />);

    await user.click(screen.getByRole("button", { name: "取消收藏“Metroid”" }));
    expect(screen.getByRole("alertdialog", { name: "取消收藏“Metroid”？" })).toHaveTextContent("1 个收藏夹");
    await user.click(screen.getByRole("button", { name: "取消收藏" }));
    await waitFor(() => expect(onChange).toHaveBeenCalledWith(null));
    await user.click(screen.getByRole("button", { name: "撤销" }));
    await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({ favoritedAtMs: 1200, folderIds: [folderId] }));
    expect(auth.fetch.mock.calls.some(([input]) => String(input).endsWith("/restore"))).toBe(true);
  });

  it("keeps the rendered state unchanged when a favorite write fails", async () => {
    auth.fetch.mockResolvedValue(json({ error: { code: "INTERNAL_ERROR", message: "temporary failure" } }, 500));
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<FavoriteActions gameId={gameId} title="Metroid" initialFavorite={null} onChange={onChange} />);

    await user.click(screen.getByRole("button", { name: "收藏“Metroid”" }));

    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("temporary failure（INTERNAL_ERROR）"));
    expect(screen.getByRole("button", { name: "收藏“Metroid”" })).toHaveAttribute("aria-pressed", "false");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("returns focus to an external card-menu anchor after Escape", async () => {
    auth.fetch.mockResolvedValue(json(page()));
    const anchor = document.createElement("button");
    anchor.textContent = "更多操作";
    document.body.append(anchor);
    const actions = createRef<FavoriteActionsHandle>();
    render(<FavoriteActions ref={actions} gameId={gameId} title="Metroid" initialFavorite={null} showManageButton={false} />);

    await act(async () => actions.current?.openFolderPicker(anchor));
    await screen.findByRole("dialog", { name: "管理“Metroid”的收藏夹" });
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    await waitFor(() => expect(anchor).toHaveFocus());
    anchor.remove();
  });

  it("creates a folder inside the picker with the current game as initial membership", async () => {
    auth.fetch.mockImplementation(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path.startsWith("/api/v1/favorites?")) return json(page());
      if (path === "/api/v1/favorite-folders") return json({ folderId: createdFolderId, name: "RPG", version: 1, visibleGameCount: 1, createdAtMs: 1000, updatedAtMs: 1000 }, 201);
      if (path === `/api/v1/favorites/${gameId}`) return json({ gameId, favoritedAtMs: 1200, folderIds: [createdFolderId] });
      throw new Error(`unexpected ${path}`);
    });
    const user = userEvent.setup();
    render(<FavoriteActions gameId={gameId} title="Metroid" initialFavorite={null} />);
    await user.click(screen.getByRole("button", { name: "管理“Metroid”的收藏夹" }));
    await user.click(await screen.findByRole("button", { name: "＋ 新建收藏夹" }));
    await user.type(screen.getByRole("textbox", { name: "收藏夹名称" }), "RPG");
    await user.click(screen.getByRole("button", { name: "创建收藏夹" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "取消收藏“Metroid”" })).toBeInTheDocument());
    expect(screen.getByRole("checkbox", { name: /RPG/ })).toBeChecked();
    const create = auth.fetch.mock.calls.find(([input]) => String(input) === "/api/v1/favorite-folders");
    expect(JSON.parse(String(create?.[1]?.body))).toEqual({ name: "RPG", initialGameIds: [gameId] });
    expect(new Headers(create?.[1]?.headers).get("Idempotency-Key")).toMatch(/^[0-9a-f-]{36}$/);
  });
});

describe("favorite dialogs", () => {
  afterEach(cleanup);

  it("exposes names, traps focus, closes with Escape, and restores prior focus", async () => {
    const close = vi.fn();
    const { rerender } = render(<><button type="button">打开</button><FolderNameDialog open={false} title="新建收藏夹" busy={false} onSubmit={vi.fn()} onClose={close} /></>);
    const trigger = screen.getByRole("button", { name: "打开" });
    trigger.focus();
    rerender(<><button type="button">打开</button><FolderNameDialog open title="新建收藏夹" busy={false} onSubmit={vi.fn()} onClose={close} /></>);
    await waitFor(() => expect(screen.getByRole("textbox", { name: "收藏夹名称" })).toHaveFocus());
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    expect(close).toHaveBeenCalledOnce();
    rerender(<><button type="button">打开</button><FolderNameDialog open={false} title="新建收藏夹" busy={false} onSubmit={vi.fn()} onClose={close} /></>);
    await waitFor(() => expect(trigger).toHaveFocus());
  });

  it("uses native multi-select checkboxes and returns an exact selection", async () => {
    const save = vi.fn();
    const user = userEvent.setup();
    render(<FolderPickerDialog open title="管理收藏夹" folders={page().folders} selectedFolderIds={[]} busy={false} onSave={save} onCreate={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByRole("dialog", { name: "管理收藏夹" })).toHaveAttribute("aria-modal", "true");
    await user.click(screen.getByRole("checkbox", { name: /想玩/ }));
    await user.click(screen.getByRole("button", { name: "完成" }));
    expect(save).toHaveBeenCalledWith([folderId]);
  });

  it("combines rename and delete in the collection edit dialog", async () => {
    const remove = vi.fn();
    const user = userEvent.setup();
    render(<FolderEditDialog open initialName="想玩" busy={false} onSubmit={vi.fn()} onDelete={remove} onClose={vi.fn()} />);
    const dialog = screen.getByRole("dialog", { name: "编辑收藏夹" });
    expect(dialog).toHaveTextContent("重命名只改变收藏夹名称");
    expect(screen.getByRole("textbox", { name: "收藏夹名称" })).toHaveValue("想玩");
    await user.click(screen.getByRole("button", { name: "删除收藏夹…" }));
    expect(remove).toHaveBeenCalledOnce();
  });
});
