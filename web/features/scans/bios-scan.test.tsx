import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import { api } from "@/lib/api/client";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { BiosScan } from "./bios-scan";
const calls = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("@/lib/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof ApiClient>();
  return { ...actual, api: { GET: calls.get, POST: calls.post } };
});
const createScan = api.POST<"/api/v1/admin/bios-scans", { body: Schema<"BiosScanRequest"> }>;
function mockCatalog(catalog: Schema<"RuntimeCatalog">) {
  calls.get.mockImplementation((path: string) => Promise.resolve({
    data: path === "/api/v1/runtime/catalog" ? catalog : { items: [] },
    response: new Response(),
  }));
}
beforeEach(() => vi.resetAllMocks());
afterEach(cleanup);
it("requires an applied directory and scope before creating the BIOS scan with its absolute path", async () => {
  mockCatalog({ platforms: [{ id: "bbc", name: "BBC Micro" }], cores: [], providers: [], bindings: [] });
  vi.mocked(createScan).mockResolvedValue({ data: { id: "scan", scanType: "bios", status: "pending", totalCount: 0, totalKnown: false, processedCount: 0, importedCount: 0, skippedCount: 0, failedCount: 0, createdAtMs: 1, updatedAtMs: 1, error: null }, response: new Response(null, { status: 201 }) });
  const onClose=vi.fn();
  const onStarted=vi.fn();
  render(<BiosScan onClose={onClose} onStarted={onStarted} />);
  expect(screen.getByRole("button", { name: "开始扫描" })).toBeDisabled();
  fireEvent.click(await screen.findByRole("checkbox", { name: "BBC Micro" }));
  expect(screen.getByRole("checkbox", { name: "全选核心范围" })).toBeDisabled();
  fireEvent.change(screen.getByLabelText("服务器目录"), { target: { value: "/mnt/bios" } });
  expect(screen.getByRole("button", { name: "开始扫描" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "进入目录" }));
  fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
  await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  expect(onStarted).toHaveBeenCalledOnce();
  expect(api.POST).toHaveBeenCalledExactlyOnceWith("/api/v1/admin/bios-scans", { body: { path: "/mnt/bios", platformIds: ["bbc"], coreIds: [] } });
  expect(await screen.findByRole("status")).toHaveTextContent("BIOS 扫描已开始");
});

it("selects all catalog items independently, supports partial selections and submits the chosen scopes", async () => {
  mockCatalog({ platforms: [{ id: "bbc", name: "BBC Micro" }, { id: "nes", name: "NES" }],
      cores: [{ id: "jsbeeb", name: "jsbeeb", platformIds: ["bbc"], fingerprint: null },
        { id: "fceumm", name: "FCEUmm", platformIds: ["nes"], fingerprint: null }], providers: [], bindings: [],
  });
  vi.mocked(createScan).mockImplementation(() => new Promise(() => {}));
  render(<BiosScan onClose={vi.fn()} />);
  const platforms = screen.getByRole("group", { name: "平台范围" });
  const cores = screen.getByRole("group", { name: "核心范围" });
  fireEvent.click(await screen.findByRole("checkbox", { name: "BBC Micro" }));
  expect(within(platforms).getByRole("checkbox", { name: "全选平台范围" })).toBePartiallyChecked();
  fireEvent.click(within(platforms).getByRole("checkbox", { name: "全选平台范围" }));
  expect(within(platforms).getByRole("checkbox", { name: "取消全选平台范围" })).toBeChecked();
  expect(within(platforms).getAllByRole("checkbox").every((item) => (item as HTMLInputElement).checked)).toBe(true);
  expect(within(cores).getAllByRole("checkbox").every((item) => !(item as HTMLInputElement).checked)).toBe(true);
  fireEvent.click(within(cores).getByRole("checkbox", { name: "全选核心范围" }));
  fireEvent.click(within(platforms).getByRole("checkbox", { name: "取消全选平台范围" }));
  expect(within(platforms).getAllByRole("checkbox").every((item) => !(item as HTMLInputElement).checked)).toBe(true);
  expect(within(cores).getAllByRole("checkbox").every((item) => (item as HTMLInputElement).checked)).toBe(true);
  fireEvent.click(within(platforms).getByRole("checkbox", { name: "NES" }));
  fireEvent.click(within(cores).getByRole("checkbox", { name: "jsbeeb" }));
  expect(within(cores).getByRole("checkbox", { name: "全选核心范围" })).toBePartiallyChecked();
  expect(within(cores).getByRole("checkbox", { name: "全选核心范围" })).toBeEnabled();
  fireEvent.click(within(cores).getByRole("checkbox", { name: "全选核心范围" }));
  fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
  await waitFor(() => expect(api.POST).toHaveBeenCalledExactlyOnceWith("/api/v1/admin/bios-scans", {
    body: { path: "/", platformIds: ["nes"], coreIds: ["jsbeeb", "fceumm"] },
  }));
  expect(within(platforms).getByRole("checkbox", { name: "全选平台范围" })).toBeDisabled();
  expect(within(cores).getByRole("checkbox", { name: "取消全选核心范围" })).toBeDisabled();
});
