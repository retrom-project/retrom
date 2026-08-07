import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PlatformManager, type Platform, type PlatformInstance } from "./platform-manager";

const navigation = vi.hoisted(() => ({ refresh: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => navigation }));

const platforms: Platform[] = [{ id: "gba", name: "Game Boy Advance", enabled: true, cores: [
  { id: "mgba", name: "mGBA", enabled: true },
  { id: "vba_next", name: "VBA Next", enabled: true }
] }];
const instances: PlatformInstance[] = [{
  id: "instance-1", platformId: "gba", platformName: "Game Boy Advance", defaultCoreId: "mgba",
  defaultCoreName: "mGBA", name: "掌机游戏", slug: "handheld", description: "", sortOrder: 10, enabled: true, version: 1, gameCount: 0
}];

describe("PlatformManager", () => {
  beforeEach(() => {
    navigation.refresh.mockReset();
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).endsWith("/default-core-preview")) {
        return new Response(JSON.stringify({ impactDigest: "impact", counts: { blocked: 0 } }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (String(input).endsWith("/order")) {
        const request = JSON.parse(String(init?.body)) as { items: Array<{ id: string; version: number }> };
        return new Response(JSON.stringify({ items: request.items.map((item, index) => ({ ...item, version: item.version + 1, sortOrder: (index + 1) * 100 })) }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ version: 2 }), { status: 200, headers: { "Content-Type": "application/json" } });
    }));
  });

  afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

  it("previews impact and only commits after the application dialog is confirmed", async () => {
    const user = userEvent.setup();
    render(<PlatformManager instances={instances} platforms={platforms} createOpen={false} />);

    const row = screen.getByText("掌机游戏").closest("tr")!;
    await user.selectOptions(within(row).getByLabelText("“掌机游戏”的推荐运行方式"), "vba_next");
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    expect(screen.getByRole("alertdialog", { name: "确认更改推荐运行方式？" })).toHaveTextContent("现有游戏不会被阻断");
    expect(navigation.refresh).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "应用更改" }));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    expect(within(row).getByLabelText("“掌机游戏”的推荐运行方式")).toHaveValue("vba_next");
  });

  it("keeps the generated URL slug out of the creation form and request", async () => {
    const user = userEvent.setup();
    render(<PlatformManager instances={[]} platforms={platforms} createOpen />);

    await user.type(screen.getByLabelText("目录名称"), "My GBA Games");
    expect(screen.queryByText("高级设置")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("网址标识")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("显示顺序")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "创建目录" }));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    const body = JSON.parse(String((fetch as ReturnType<typeof vi.fn>).mock.calls[0][1]?.body)) as Record<string, unknown>;
    expect(body).toMatchObject({ platformId: "gba", defaultCoreId: "mgba", name: "My GBA Games" });
    expect(body).not.toHaveProperty("slug");
  });

  it("persists keyboard reordering without exposing numeric sort fields", async () => {
    const user = userEvent.setup();
    const second: PlatformInstance = { ...instances[0], id: "instance-2", name: "另一个目录", slug: "other", sortOrder: 20 };
    render(<PlatformManager instances={[...instances, second]} platforms={platforms} createOpen={false} />);

    const handle = screen.getByRole("button", { name: "拖动“另一个目录”调整顺序" });
    handle.focus();
    await user.keyboard("{ArrowUp}");
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    const body = JSON.parse(String((fetch as ReturnType<typeof vi.fn>).mock.calls[0][1]?.body)) as { items: Array<{ id: string }> };
    expect(body.items.map((item) => item.id)).toEqual(["instance-2", "instance-1"]);
    expect(screen.queryByLabelText("显示顺序")).not.toBeInTheDocument();
    expect(screen.queryByText("目录顺序已保存。")).not.toBeInTheDocument();
  });

  it("lets a non-empty directory change user visibility without showing success banners", async () => {
    const user = userEvent.setup();
    render(<PlatformManager instances={[{ ...instances[0], gameCount: 3 }]} platforms={platforms} createOpen={false} />);

    const enabled = screen.getByRole("checkbox", { name: "“掌机游戏”启用状态" });
    expect(enabled).toBeEnabled();
    await user.click(enabled);
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    expect(screen.queryByText(/目录“掌机游戏”已更新/)).not.toBeInTheDocument();
  });

  it("uses a single-row editor for the user-facing description", async () => {
    const user = userEvent.setup();
    render(<PlatformManager instances={instances} platforms={platforms} createOpen={false} />);

    await user.click(screen.getByRole("button", { name: "修改“掌机游戏”给用户看的说明" }));
    const row = screen.getByText("掌机游戏").closest("tr")!;
    expect(within(row).getByRole("textbox", { name: "给用户看的说明" })).toHaveAttribute("rows", "1");
  });
});
