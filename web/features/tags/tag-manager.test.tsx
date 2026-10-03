import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { render } from "@/components/toast-test-utils";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TagManager, type TagAdminItem, type TagAdminPage } from "./tag-manager";

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const initialTag: TagAdminItem = {
  tagId: "01980000-0000-7000-8000-000000000901", name: "动作", status: "ACTIVE", version: 2,
  usage: { publishedGameCount: 3, deletedGameCount: 1, reviewDraftCount: 2, sourceCollectionCount: 4 },
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
  it("preserves server collation and cursor when common tags already exist", async () => {
    const ordered = ["Zebra", "ä", "中", "动作"].map((name, index) => ({ ...initialTag, tagId: `tag-${index}`, name }));
    const fetchMock = vi.fn().mockResolvedValueOnce(json({ createdItems: [], existingItems: ordered }))
      .mockResolvedValueOnce(json({ ...initial, items: [{ ...initialTag, name: "蛇", tagId: "last" }], nextCursor: null }));
    vi.stubGlobal("fetch", fetchMock);
    render(<TagManager initial={{ ...initial, items: ordered, nextCursor: "server-cursor" }} filters={{ q: "", status: "ACTIVE", sort: "NAME_ASC" }} />);
    await userEvent.click(screen.getByRole("button", { name: "添加常用标签" }));
    expect(await screen.findByRole("status")).toHaveTextContent("4 个常用标签已全部存在");
    expect(screen.getAllByRole("rowheader").map((row) => row.textContent)).toEqual(ordered.map((item) => item.name));
    expect(fetchMock).toHaveBeenCalledOnce();
    await userEvent.click(screen.getByRole("button", { name: "加载更多" }));
    await screen.findByRole("rowheader", { name: "蛇" });
    expect(fetchMock.mock.calls[1][0]).toContain("cursor=server-cursor");
    expect(screen.getAllByRole("rowheader").map((row) => row.textContent)).toEqual([...ordered.map((item) => item.name), "蛇"]);
  });

  it("uses the refreshed server filter/order after rename and replaces the old cursor", async () => {
    const renamed = { ...initialTag, name: "Renamed", version: 3 };
    const fetchMock = vi.fn().mockResolvedValueOnce(json(renamed))
      .mockResolvedValueOnce(json({ ...initial, items: [], nextCursor: null }));
    vi.stubGlobal("fetch", fetchMock);
    render(<TagManager initial={{ ...initial, nextCursor: "stale" }} filters={{ q: "动作", status: "ACTIVE", sort: "UPDATED_DESC" }} />);
    await userEvent.click(screen.getByRole("button", { name: "编辑" }));
    const name = screen.getByRole("textbox", { name: "标签名称" });
    fireEvent.change(name, { target: { value: "Renamed" } });
    await userEvent.click(screen.getByRole("button", { name: "保存标签" }));
    await waitFor(() => expect(screen.queryByRole("rowheader")).toBeNull());
    expect(screen.queryByRole("button", { name: "加载更多" })).toBeNull();
    const query = new URL(fetchMock.mock.calls[1][0], "http://localhost").searchParams;
    expect(query.get("q")).toBe("动作"); expect(query.get("sort")).toBe("UPDATED_DESC"); expect(query.has("cursor")).toBe(false);
  });

  it("keeps the create drawer open for repeated additions and restores input focus", async () => {
    const items = [initialTag];
    const fetchMock = vi.fn().mockImplementation(async (_url, init) => {
      if (!init?.body) {return json({ ...initial, items: [...items] });}
      const saved = { ...initialTag, tagId: JSON.parse(init.body).name, name: JSON.parse(init.body).name };
      items.push(saved);
      return json(saved, 201);
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<TagManager initial={initial} filters={{ q: "", status: "ACTIVE", sort: "NAME_ASC" }} />);
    await user.click(screen.getByRole("button", { name: "新建标签" }));
    const sheet = screen.getByRole("dialog", { name: "新建标签" });
    const input = within(sheet).getByRole("textbox", { name: "标签名称" });
    for (const name of ["双人", "精选"]) {
      await user.type(input, name);
      await user.click(within(sheet).getByRole("button", { name: "保存标签" }));
      await waitFor(() => expect(input).toHaveValue(""));
      expect(sheet).toBeVisible(); await waitFor(() => expect(input).toHaveFocus());
      expect(screen.getByRole("status")).toHaveTextContent(`已创建“${name}”，可继续添加。`);
    }
    expect(fetchMock).toHaveBeenCalledTimes(4);
    await user.click(within(sheet).getByRole("button", { name: "取消" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByRole("rowheader", { name: "精选" })).toBeVisible();
  });

  it("atomically adds the common tag template and reports existing items", async () => {
    const createdItems = [
      { ...initialTag, tagId: "01980000-0000-7000-8000-000000000910", name: "动作冒险", version: 1, usage: { publishedGameCount: 0, deletedGameCount: 0, reviewDraftCount: 0, sourceCollectionCount: 0 } },
      { ...initialTag, tagId: "01980000-0000-7000-8000-000000000911", name: "益智解谜", version: 1, usage: { publishedGameCount: 0, deletedGameCount: 0, reviewDraftCount: 0, sourceCollectionCount: 0 } },
    ];
    const fetchMock = vi.fn().mockResolvedValueOnce(json({ createdItems, existingItems: [initialTag] })).mockResolvedValueOnce(json({ ...initial, items: [initialTag, ...createdItems], summary: { ...initial.summary, activeTagCount: 3 } }));
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
    const created: TagAdminItem = { ...initialTag, tagId: "01980000-0000-7000-8000-000000000902", name: "双人", version: 1, usage: { publishedGameCount: 0, deletedGameCount: 0, reviewDraftCount: 0, sourceCollectionCount: 0 } };
    const renamed = { ...initialTag, name: "动作游戏", version: 3 };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json(created, 201))
      .mockResolvedValueOnce(json({ ...initial, items: [initialTag, created], summary: { ...initial.summary, activeTagCount: 2 } }))
      .mockResolvedValueOnce(json(renamed))
      .mockResolvedValueOnce(json({ ...initial, items: [renamed, created] }))
      .mockResolvedValueOnce(new Response(null, { status: 204, headers: { ETag: '"v2"' } }))
      .mockResolvedValueOnce(json({ ...initial, items: [created] }));
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

    await user.click(within(createSheet).getByRole("button", { name: "取消" }));
    const actionRow = screen.getByRole("rowheader", { name: "动作" }).closest("tr");
    if (!actionRow) {throw new Error("action row missing");}
    await user.click(within(actionRow).getByRole("button", { name: "编辑" }));
    const editSheet = screen.getByRole("dialog", { name: "编辑标签" });
    const editName = within(editSheet).getByRole("textbox", { name: "标签名称" });
    await user.clear(editName);
    await user.type(editName, "动作游戏");
    await user.click(within(editSheet).getByRole("button", { name: "保存标签" }));
    await waitFor(() => expect(fetchMock).toHaveBeenNthCalledWith(3, `/api/v1/admin/tags/${initialTag.tagId}`, expect.objectContaining({
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
    await waitFor(() => expect(fetchMock).toHaveBeenNthCalledWith(5, `/api/v1/admin/tags/${initialTag.tagId}`, expect.objectContaining({
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
    expect(await screen.findByRole("alert")).toHaveTextContent("已存在同名活动标签");
    expect(within(sheet).queryByRole("alert")).toBeNull();
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveValue("Action Duplicate");
  });
});
