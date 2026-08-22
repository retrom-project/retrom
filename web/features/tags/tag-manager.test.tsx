import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TagManager, type TagAdminItem, type TagAdminPage } from "./tag-manager";

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const initialTag: TagAdminItem = {
  tagId: "01980000-0000-7000-8000-000000000901", name: "动作", status: "ACTIVE", version: 2,
  usage: { publishedGameCount: 3, deletedGameCount: 1, reviewDraftCount: 2, pegasusCollectionCount: 4 },
  createdAtMs: 1_000, updatedAtMs: 2_000, deletedAtMs: null,
};

const initial: TagAdminPage = {
  summary: { activeTagCount: 1, taggedGameCount: 3, pendingReviewCount: 2 },
  items: [initialTag], nextCursor: null,
};

function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
}

describe("TagManager", () => {
  it("atomically adds the common tag template and reports existing items", async () => {
    const createdItems = [
      { ...initialTag, tagId: "01980000-0000-7000-8000-000000000910", name: "动作冒险", version: 1, usage: { publishedGameCount: 0, deletedGameCount: 0, reviewDraftCount: 0, pegasusCollectionCount: 0 } },
      { ...initialTag, tagId: "01980000-0000-7000-8000-000000000911", name: "益智解谜", version: 1, usage: { publishedGameCount: 0, deletedGameCount: 0, reviewDraftCount: 0, pegasusCollectionCount: 0 } },
    ];
    const fetchMock = vi.fn().mockResolvedValue(json({ createdItems, existingItems: [initialTag] }));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    const { container } = render(<TagManager initial={initial} filters={{ q: "", status: "ACTIVE", sort: "NAME_ASC" }} />);

    await user.click(screen.getByRole("button", { name: "添加常用标签" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/tags/defaults", expect.objectContaining({
      method: "POST", body: JSON.stringify({}), headers: expect.objectContaining({ "Idempotency-Key": expect.any(String) }),
    })));
    expect(await screen.findByText("已添加 2 个常用标签，1 个已存在。")).toBeVisible();
    expect(screen.getByRole("rowheader", { name: "动作冒险" })).toBeVisible();
    expect(screen.getByRole("rowheader", { name: "益智解谜" })).toBeVisible();
    expect(container.querySelector(".tag-kpis article:first-child strong")).toHaveTextContent("3");
  });

  it("creates, renames and name-confirms a soft delete using optimistic versions", async () => {
    const created: TagAdminItem = { ...initialTag, tagId: "01980000-0000-7000-8000-000000000902", name: "双人", version: 1, usage: { publishedGameCount: 0, deletedGameCount: 0, reviewDraftCount: 0, pegasusCollectionCount: 0 } };
    const renamed = { ...initialTag, name: "动作游戏", version: 3 };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json(created, 201))
      .mockResolvedValueOnce(json(renamed))
      .mockResolvedValueOnce(new Response(null, { status: 204, headers: { ETag: '"v2"' } }));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    const { container } = render(<TagManager initial={initial} filters={{ q: "", status: "ACTIVE", sort: "NAME_ASC" }} />);

    expect(screen.getByText("3 / 1")).toHaveAttribute("href", expect.stringContaining(`tagId=${initialTag.tagId}`));
    await user.click(screen.getByRole("button", { name: "新建标签" }));
    const createSheet = screen.getByRole("dialog", { name: "新建标签" });
    const createName = within(createSheet).getByRole("textbox", { name: "标签名称" });
    await waitFor(() => expect(createName).toHaveFocus());
    await user.type(createName, "  双人  ");
    expect(within(createSheet).getByText("双人")).toBeVisible();
    await user.click(within(createSheet).getByRole("button", { name: "保存标签" }));
    await waitFor(() => expect(fetchMock).toHaveBeenNthCalledWith(1, "/api/v1/admin/tags", expect.objectContaining({
      method: "POST", body: JSON.stringify({ name: "  双人  " }),
    })));
    expect(await screen.findByText("双人")).toBeVisible();
    expect(container.querySelector(".tag-kpis article:first-child strong")).toHaveTextContent("2");

    const actionRow = screen.getByRole("rowheader", { name: "动作" }).closest("tr");
    if (!actionRow) {throw new Error("action row missing");}
    await user.click(within(actionRow).getByRole("button", { name: "编辑" }));
    const editSheet = screen.getByRole("dialog", { name: "编辑标签" });
    const editName = within(editSheet).getByRole("textbox", { name: "标签名称" });
    await user.clear(editName);
    await user.type(editName, "动作游戏");
    await user.click(within(editSheet).getByRole("button", { name: "保存标签" }));
    await waitFor(() => expect(fetchMock).toHaveBeenNthCalledWith(2, `/api/v1/admin/tags/${initialTag.tagId}`, expect.objectContaining({
      method: "PATCH", headers: expect.objectContaining({ "If-Match": '"v2"' }), body: JSON.stringify({ name: "动作游戏" }),
    })));

    const renamedRow = (await screen.findByRole("rowheader", { name: "动作游戏" })).closest("tr");
    if (!renamedRow) {throw new Error("renamed row missing");}
    await user.click(within(renamedRow).getByRole("button", { name: "删除" }));
    const dialog = screen.getByRole("alertdialog", { name: "删除标签" });
    const confirm = within(dialog).getByRole("button", { name: "删除标签" });
    expect(confirm).toBeDisabled();
    expect(within(dialog).getByText(/3 个已发布游戏、1 个已删除游戏、2 个待审核草稿、4 个扫描映射/)).toBeVisible();
    await user.type(within(dialog).getByRole("textbox", { name: /输入完整名称“动作游戏”确认/ }), "动作游戏");
    expect(confirm).toBeEnabled();
    await user.click(confirm);
    await waitFor(() => expect(fetchMock).toHaveBeenNthCalledWith(3, `/api/v1/admin/tags/${initialTag.tagId}`, expect.objectContaining({
      method: "DELETE", headers: expect.objectContaining({ "If-Match": '"v3"' }), body: JSON.stringify({ confirmName: "动作游戏" }),
    })));
    await waitFor(() => expect(screen.queryByRole("rowheader", { name: "动作游戏" })).not.toBeInTheDocument());
  });

  it("keeps the editor input visible when the API reports a name conflict", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(json({ error: { code: "TAG_NAME_CONFLICT", message: "已存在同名活动标签" } }, 409)));
    const user = userEvent.setup();
    render(<TagManager initial={initial} filters={{ q: "", status: "ACTIVE", sort: "NAME_ASC" }} />);
    await user.click(screen.getByRole("button", { name: "新建标签" }));
    const sheet = screen.getByRole("dialog", { name: "新建标签" });
    const input = within(sheet).getByRole("textbox", { name: "标签名称" });
    fireEvent.change(input, { target: { value: "Action Duplicate" } });
    await user.click(within(sheet).getByRole("button", { name: "保存标签" }));
    expect(await within(sheet).findByText(/已存在同名活动标签/)).toBeVisible();
    expect(input).toHaveValue("Action Duplicate");
  });
});
