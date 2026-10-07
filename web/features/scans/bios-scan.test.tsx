import { fireEvent, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import { api } from "@/lib/api/client";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { BiosScan } from "./bios-scan";
vi.mock("@/lib/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof ApiClient>();
  return { ...actual, api: { GET: vi.fn(), POST: vi.fn() } };
});
const getCatalog = api.GET<"/api/v1/runtime/catalog", Record<string, never>>;
const createScan = api.POST<"/api/v1/admin/bios-scans", { body: Schema<"BiosScanRequest"> }>;
it("requires an applied directory and scope before creating the BIOS scan with its absolute path", async () => {
  const data = { items: [], platforms: [{ id: "bbc", name: "BBC Micro" }], cores: [], providers: [], bindings: [] };
  vi.mocked(getCatalog).mockResolvedValue({ data, response: new Response() });
  vi.mocked(createScan).mockResolvedValue({ data: { id: "scan", scanType: "bios", status: "pending", totalCount: 0, totalKnown: false, processedCount: 0, importedCount: 0, skippedCount: 0, failedCount: 0, createdAtMs: 1, updatedAtMs: 1, error: null }, response: new Response(null, { status: 201 }) });
  const onClose=vi.fn();
  render(<BiosScan onClose={onClose} />);
  expect(screen.getByRole("button", { name: "开始扫描" })).toBeDisabled();
  fireEvent.click(await screen.findByRole("checkbox", { name: "BBC Micro" }));
  fireEvent.change(screen.getByLabelText("服务器目录"), { target: { value: "/mnt/bios" } });
  expect(screen.getByRole("button", { name: "开始扫描" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "进入目录" }));
  fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
  await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  expect(api.POST).toHaveBeenCalledExactlyOnceWith("/api/v1/admin/bios-scans", { body: { path: "/mnt/bios", platformIds: ["bbc"], coreIds: [] } });
  expect(await screen.findByRole("status")).toHaveTextContent("BIOS 扫描已开始");
});
