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
  defaultCoreName: "mGBA", name: "掌机游戏", slug: "handheld", description: "", sortOrder: 10, enabled: true, version: 1
}];

describe("PlatformManager", () => {
  beforeEach(() => {
    navigation.refresh.mockReset();
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).endsWith("/default-core-preview")) {
        return new Response(JSON.stringify({ impactDigest: "impact", counts: { blocked: 0 } }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
    }));
  });

  afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

  it("does not preview on selection and always asks before committing a default core", async () => {
    const user = userEvent.setup();
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<PlatformManager instances={instances} platforms={platforms} createOpen={false} />);

    const row = screen.getByText("掌机游戏").closest("tr");
    expect(row).not.toBeNull();
    await user.click(within(row!).getByText("编辑"));
    await user.selectOptions(within(row!).getByLabelText("默认核心"), "vba_next");
    expect(fetch).not.toHaveBeenCalled();

    await user.click(within(row!).getByRole("button", { name: "预览并更改默认核心" }));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining("不会阻断现有游戏"));
    expect(navigation.refresh).not.toHaveBeenCalled();
  });
});
