import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import { api } from "@/lib/api/client";
import type * as ApiClient from "@/lib/api/client";
import { TagManager } from "./tag-manager";

vi.mock("@/lib/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof ApiClient>();
  return { ...actual, api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn(), DELETE: vi.fn() } };
});

const tag = { id: "tag-one", name: "动作游戏", version: 1, gameCount: 0 };
const getTags = api.GET<"/api/v1/admin/tags", { params: { query: { offset: number; limit: number; q: string } } }>;
beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(getTags).mockResolvedValue({
    data: { items: [tag], total: 1, offset: 0, limit: 100 }, response: new Response(),
  });
});

it("keeps a duplicate name editable and reports one toast before a successful retry", async () => {
  vi.mocked(api.POST).mockResolvedValueOnce({
    error: { code: "TAG_NAME_CONFLICT", message: "A tag with this name already exists" },
    response: new Response(null, { status: 409 }),
  } as Awaited<ReturnType<typeof api.POST>>).mockResolvedValueOnce({
    data: { ...tag, id: "tag-two", name: "冒险游戏" }, response: new Response(),
  } as Awaited<ReturnType<typeof api.POST>>);
  render(<TagManager />);
  fireEvent.click(screen.getByRole("button", { name: "新建标签" }));
  fireEvent.change(screen.getByLabelText("标签名称"), { target: { value: tag.name } });
  fireEvent.click(screen.getByRole("button", { name: "保存标签" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("已存在同名标签，请使用其他名称。");
  expect(screen.getAllByText("已存在同名标签，请使用其他名称。")).toHaveLength(1);
  expect(screen.getByRole("dialog", { name: "新建标签" })).toBeVisible();
  expect(screen.getByLabelText("标签名称")).toHaveValue(tag.name);
  fireEvent.change(screen.getByLabelText("标签名称"), { target: { value: "冒险游戏" } });
  fireEvent.click(screen.getByRole("button", { name: "保存标签" }));
  await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("标签已创建，可以继续添加。"));
  expect(screen.getByLabelText("标签名称")).toHaveValue("");
  expect(api.POST).toHaveBeenLastCalledWith("/api/v1/admin/tags", { body: { name: "冒险游戏" } });
});

it("does not mislabel a stale edit as a duplicate name", async () => {
  vi.mocked(api.PATCH).mockResolvedValue({
    error: { code: "VERSION_CONFLICT", message: "Item changed; refresh and try again" },
    response: new Response(null, { status: 409 }),
  } as Awaited<ReturnType<typeof api.PATCH>>);
  render(<TagManager />);
  fireEvent.click(await screen.findByRole("button", { name: "编辑" }));
  fireEvent.change(screen.getByLabelText("标签名称"), { target: { value: "新名称" } });
  fireEvent.click(screen.getByRole("button", { name: "保存标签" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("标签已在其他页面修改，请刷新列表后重试。");
  expect(screen.queryByText("已存在同名标签，请使用其他名称。")).not.toBeInTheDocument();
  expect(screen.getByLabelText("标签名称")).toHaveValue("新名称");
});
