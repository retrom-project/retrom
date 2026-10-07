import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { GameScan } from "./game-scan";

const calls = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("next/navigation", () => ({
  useRouter: () => ({
    push: (path: string) => window.history.pushState(null, "", path),
  }),
}));
vi.mock("@/lib/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof ApiClient>();
  return { ...actual, api: { GET: calls.get, POST: calls.post } };
});

const pendingScan: Schema<"ScanProgress"> = {
  id: "scan", scanType: "game", status: "pending", totalCount: 0,
  totalKnown: false, processedCount: 0, importedCount: 0, skippedCount: 0,
  failedCount: 0, createdAtMs: 1, updatedAtMs: 1, error: null,
};
beforeEach(() => {
  vi.resetAllMocks();
  window.history.replaceState(null, "", "/admin/imports/server");
  calls.get.mockImplementation((path: string) => Promise.resolve({
    data: { items: path === "/api/v1/platform-instances"
      ? [{ id: "directory", name: "NES", enabled: true }]
      : [] },
    response: new Response(),
  }));
  calls.post.mockResolvedValueOnce({
    data: { items: [{ key: "collection", name: "测试集合", path: "/games", gameCount: 1 }] },
    response: new Response(),
  });
});
afterEach(cleanup);

async function prepareMapping(format: "pegasus" | "emulationstation") {
  render(<GameScan />);
  fireEvent.click(screen.getByRole("button", {
    name: `选择 ${format === "pegasus" ? "Pegasus" : "EmulationStation"} 目录`,
  }));
  fireEvent.click(screen.getByRole("button", { name: "读取来源集合" }));
  const directory = await screen.findByRole("combobox", { name: "测试集合游戏目录" });
  fireEvent.change(directory, { target: { value: "directory" } });
  return screen.getByRole("button", { name: "开始扫描" });
}

it.each(["pegasus", "emulationstation"] as const)(
  "navigates to the unfiltered review queue as soon as the %s scan is accepted",
  async (format) => {
    const start = await prepareMapping(format);
    const response = Promise.withResolvers<{ data: Schema<"ScanProgress">; response: Response }>();
    calls.post.mockReturnValueOnce(response.promise);
    fireEvent.click(start);
    expect(start).toBeDisabled();
    expect(window.location.pathname).toBe("/admin/imports/server");
    expect(calls.post).toHaveBeenLastCalledWith("/api/v1/admin/game-scans", {
      body: { path: "/", format, mappings: [{ sourceKey: "collection", platformInstanceId: "directory", tagIds: [] }] },
    });
    await act(async () => response.resolve({
      data: pendingScan,
      response: new Response(null, { status: 201 }),
    }));
    await waitFor(() => expect(window.location.pathname + window.location.search).toBe("/admin/reviews?scanId=scan"));
    expect(screen.getByRole("status")).toHaveTextContent("扫描已开始。接收完成的游戏将进入统一待审核列表。");
  },
);

it.each([400, 500])("keeps the mapping and permits retry after a %s scan rejection", async (status) => {
  const start = await prepareMapping("pegasus");
  calls.post.mockResolvedValueOnce({
    error: { code: "SCAN_REJECTED", message: "无法开始本次扫描。" },
    response: new Response(null, { status }),
  });
  fireEvent.click(start);
  expect(await screen.findByRole("alert")).toHaveTextContent("无法开始本次扫描。");
  expect(window.location.pathname + window.location.search).toBe("/admin/imports/server");
  expect(screen.getByRole("combobox", { name: "测试集合游戏目录" })).toHaveValue("directory");
  expect(start).toBeEnabled();
});
