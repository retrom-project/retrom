import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PlatformManager, type Platform, type PlatformInstance, type PlatformRecommendations } from "./platform-manager";

const navigation = vi.hoisted(() => ({ refresh: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => navigation }));

const platforms: Platform[] = [{ id: "gba", name: "Game Boy Advance", enabled: true, cores: [
  { id: "mgba", name: "mGBA", enabled: true, },
  { id: "vba_next", name: "VBA Next", enabled: true, }
] }];
const instances: PlatformInstance[] = [{
  id: "instance-1", platformId: "gba", platformName: "Game Boy Advance", defaultCoreId: "mgba",
  defaultCoreName: "mGBA", name: "掌机游戏", slug: "handheld", description: "", createdAtMs: 10, enabled: true, version: 1, gameCount: 0,
  supportedExtensions: [".gba"]
}];
const recommendations: PlatformRecommendations = {
  catalogVersion: 1,
  summary: { totalCount: 1, activeCount: 0, customizedCount: 0, coveredByEquivalentCount: 0, suppressedCount: 0, missingCount: 1 },
  items: [{
    templateKey: "gba/mgba", catalogOrder: 40, name: "GBA 游戏", description: "",
    platform: { id: "gba", name: "Game Boy Advance" }, defaultCore: { id: "mgba", name: "mGBA" },
    supportedExtensions: [".gba"], state: "MISSING", platformInstanceId: null,
  }],
};

describe("PlatformManager", () => {
  beforeEach(() => {
    navigation.refresh.mockReset();
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).endsWith("/default-core-preview")) {
        return new Response(JSON.stringify({ impactDigest: "impact", counts: { ready: 1, needsValidation: 0, blocked: 0 } }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (String(input).endsWith("/recommendations/apply")) {
        return new Response(JSON.stringify({
          catalogVersion: 1,
          createdTemplateKeys: ["gba/mgba"],
          created: [{ ...instances[0], id: "recommended-gba", name: "GBA 游戏", slug: "gba-games", createdAtMs: 1, updatedAtMs: 1 }],
          summary: { createdCount: 1, coveredCount: 0, suppressedCount: 0, remainingMissingCount: 0 },
          items: [{ ...recommendations.items[0], state: "ACTIVE", platformInstanceId: "11111111-1111-4111-8111-111111111111" }],
        }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (init?.method === "POST") {
        return new Response(JSON.stringify({ ...instances[0], id: "created", name: "My GBA Games", createdAtMs: 30 }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ version: 2 }), { status: 200, headers: { "Content-Type": "application/json" } });
    }));
  });

  afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

  it("groups directories under collapsible category headings without order fields", async () => {
    const user = userEvent.setup();
    render(<PlatformManager instances={instances} platforms={platforms} createOpen={false} />);
    const group = screen.getByRole("button", { name: "掌机 1 个目录" });
    expect(group).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("掌机游戏")).not.toBeInTheDocument();
    group.focus();
    await user.keyboard("{Enter}");
    expect(group).toHaveAttribute("aria-expanded", "true");
    expect(screen.getAllByRole("columnheader").map((header) => header.textContent)).toEqual(["游戏目录", "游戏平台", "扩展名", "游戏数", "推荐运行方式", "启用状态", "操作"]);
    expect(screen.getByRole("cell", { name: "Game Boy Advance 支持的扩展名" })).toHaveTextContent(".gba");
    await user.click(screen.getByRole("button", { name: "全部收起" }));
    expect(screen.queryByText("掌机游戏")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "全部展开" }));
    expect(screen.getByText("掌机游戏")).toBeVisible();
  });

  it("previews impact and only commits after the application dialog is confirmed", async () => {
    const user = userEvent.setup();
    render(<PlatformManager instances={instances} platforms={platforms} createOpen={false} />);

    fireEvent.click(screen.getByRole("button", { name: "全部展开" }));
    const row = screen.getByText("掌机游戏").closest<HTMLElement>("[role='row']")!;
    await user.selectOptions(within(row).getByLabelText("“掌机游戏”的推荐运行方式"), "vba_next");
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    expect(screen.getByRole("alertdialog", { name: "确认更改推荐运行方式？" })).toHaveTextContent("没有游戏会被阻断");
    expect(navigation.refresh).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "应用更改" }));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    expect(within(row).getByLabelText("“掌机游戏”的推荐运行方式")).toHaveValue("vba_next");
  });

  it("keeps the generated URL slug out of the creation form and request", async () => {
    const user = userEvent.setup();
    render(<PlatformManager instances={[]} platforms={platforms} createOpen />);

    await user.type(screen.getByLabelText("目录名称"), "My GBA Games");
    expect(screen.getByRole("dialog", { name: "新建游戏目录" })).toBeInTheDocument();
    expect(screen.queryByText("新建平台实例")).not.toBeInTheDocument();
    expect(screen.queryByText("高级设置")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("网址标识")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("显示顺序")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "创建目录" }));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    const body = JSON.parse(String((fetch as ReturnType<typeof vi.fn>).mock.calls[0][1]?.body)) as Record<string, unknown>;
    expect(body).toMatchObject({ platformId: "gba", defaultCoreId: "mgba", name: "My GBA Games" });
    expect(body).not.toHaveProperty("slug");
    expect(body).not.toHaveProperty("sortOrder");
    expect(screen.getByRole("button", { name: "掌机 1 个目录" })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("My GBA Games")).toBeVisible();
  });


  it("lets a non-empty directory change user visibility without showing success banners", async () => {
    const user = userEvent.setup();
    render(<PlatformManager instances={[{ ...instances[0], gameCount: 3 }]} platforms={platforms} createOpen={false} />);

    fireEvent.click(screen.getByRole("button", { name: "全部展开" }));
    const enabled = screen.getByRole("checkbox", { name: "“掌机游戏”启用状态" });
    expect(enabled).toBeEnabled();
    await user.click(enabled);
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    expect(screen.queryByText(/目录“掌机游戏”已更新/)).not.toBeInTheDocument();
  });

  it("uses a single-row editor for the user-facing description", async () => {
    const user = userEvent.setup();
    render(<PlatformManager instances={instances} platforms={platforms} createOpen={false} />);

    fireEvent.click(screen.getByRole("button", { name: "全部展开" }));
    await user.click(screen.getByRole("button", { name: "管理目录“掌机游戏”" }));
    await user.click(screen.getByRole("menuitem", { name: "编辑说明" }));
    fireEvent.click(screen.getByRole("button", { name: "全部展开" }));
    const row = screen.getByText("掌机游戏").closest<HTMLElement>("[role='row']")!;
    expect(within(row).getByRole("textbox", { name: "给用户看的说明" })).toHaveAttribute("rows", "1");
  });

  it("searches every category and restores collapsed state after clearing", async () => {
    const user = userEvent.setup();
    const second: PlatformInstance = { ...instances[0], id: "instance-2", name: "街机目录", slug: "arcade", platformId: "arcade", platformName: "Arcade" };
    render(<PlatformManager instances={[...instances, second]} platforms={platforms} createOpen={false} />);

    await user.type(screen.getByRole("searchbox", { name: "搜索目录" }), "街机");
    expect(screen.getByText("街机目录")).toBeInTheDocument();
    expect(screen.queryByText("掌机游戏")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "街机 1 个目录" })).toHaveAttribute("aria-expanded", "true");
    await user.clear(screen.getByRole("searchbox", { name: "搜索目录" }));
    expect(screen.getByRole("button", { name: "街机 1 个目录" })).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("街机目录")).not.toBeInTheDocument();
  });

  it("filters by the status dropdown without duplicate quick filters", async () => {
    const user = userEvent.setup();
    const disabled: PlatformInstance = { ...instances[0], id: "instance-2", name: "停用目录", slug: "disabled", enabled: false, platformId: "arcade", platformName: "Arcade" };
    render(<PlatformManager instances={[...instances, disabled]} platforms={platforms} createOpen={false} />);

    expect(screen.queryByLabelText("游戏目录快速筛选")).not.toBeInTheDocument();
    const status = screen.getByRole("combobox", { name: "启用状态" });
    await user.selectOptions(status, "DISABLED");
    expect(status).toHaveValue("DISABLED");
    expect(screen.getByText("停用目录")).toBeInTheDocument();
    expect(screen.queryByText("掌机游戏")).not.toBeInTheDocument();
  });


  it("keeps a single-row menu open while its table scrolls and restores focus on Escape", async () => {
    const user = userEvent.setup();
    render(<PlatformManager instances={instances} platforms={platforms} createOpen={false} />);
    await user.click(screen.getByRole("button", { name: "全部展开" }));
    const trigger = screen.getByRole("button", { name: "管理目录“掌机游戏”" });
    await user.click(trigger);
    fireEvent.scroll(window);
    expect(screen.getByRole("menu")).toBeVisible();
    await user.keyboard("{ArrowDown}");
    expect(screen.getByRole("menuitem", { name: "编辑说明" })).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
    await user.click(trigger);
    await user.click(trigger);
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("opens the creation drawer from the page header and closes it from the backdrop", async () => {
    const user = userEvent.setup();
    render(<PlatformManager instances={instances} platforms={platforms} createOpen={false} />);

    await user.click(screen.getByRole("button", { name: "新建游戏目录" }));
    expect(screen.getByRole("dialog", { name: "新建游戏目录" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "关闭新建游戏目录" }));
    expect(screen.queryByRole("dialog", { name: "新建游戏目录" })).not.toBeInTheDocument();
  });

  it("keeps an empty page explicit and applies missing recommendations once", async () => {
    const user = userEvent.setup();
    render(<PlatformManager instances={[]} platforms={platforms} recommendations={recommendations} createOpen={false} />);

    expect(screen.queryByRole("dialog", { name: "新建游戏目录" })).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "还没有游戏目录" })).toBeInTheDocument();
    const buttons = screen.getAllByRole("button", { name: "一键创建推荐目录 1" });
    await user.click(buttons[0]);
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    const [path, options] = (fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(path).toBe("/api/v1/admin/platform-instances/recommendations/apply");
    expect(options).toMatchObject({ method: "POST", body: "{}" });
    expect(await screen.findByText("GBA 游戏")).toBeInTheDocument();
    expect(screen.getByText("已创建 1 个；已有 0 个目录保持不变。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "✓ 推荐目录已创建" })).toBeDisabled();
    expect(navigation.refresh).toHaveBeenCalledTimes(1);
  });

  it("reports an atomic recommendation failure and keeps the empty state", async () => {
    const user = userEvent.setup();
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ error: { message: "服务暂时不可用" } }), {
      status: 500,
      headers: { "Content-Type": "application/json" },
    }));
    render(<PlatformManager instances={[]} platforms={platforms} recommendations={recommendations} createOpen={false} />);

    await user.click(screen.getAllByRole("button", { name: "一键创建推荐目录 1" })[0]);
    expect(await screen.findByRole("alert")).toHaveTextContent("服务暂时不可用");
    expect(screen.getByRole("heading", { name: "还没有游戏目录" })).toBeInTheDocument();
  });

  it("explains suppressed recommendations when every item is handled", () => {
    render(<PlatformManager instances={instances} platforms={platforms} recommendations={{
      catalogVersion: 1,
      summary: { totalCount: 1, activeCount: 0, customizedCount: 0, coveredByEquivalentCount: 0, suppressedCount: 1, missingCount: 0 },
      items: [{ ...recommendations.items[0], state: "SUPPRESSED", platformInstanceId: "11111111-1111-4111-8111-111111111111" }],
    }} createOpen={false} />);

    expect(screen.getByRole("button", { name: "✓ 推荐目录已创建" })).toHaveAttribute("title", expect.stringContaining("1 个已停用或删除"));
  });
});
