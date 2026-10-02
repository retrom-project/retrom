import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import ImportOverviewPage from "@/app/admin/imports/page";

vi.mock("@/lib/server-backend", () => ({
  backendJSON: async (path: string) => path.endsWith("/summary") ? {} : {
    items: path.includes("source-imports") ? [] : [{
      id: "batch", state: "COMPLETED", platformInstanceName: "NES 游戏",
      metadataProvider: "NONE", totalItemCount: 1, reviewPendingItemCount: 0,
      failedItemCount: 0, rejectedFileCount: 0, version: 1,
      createdAtMs: Date.parse("2026-10-01T19:35:00Z"), updatedAtMs: Date.parse("2026-10-01T19:35:00Z"),
    }], nextCursor: null,
  },
}));

describe("recent import browser time", () => {
  const originalTimeZone = process.env.TZ;
  afterEach(() => { process.env.TZ = originalTimeZone; });

  it("displays the batch creation time in the browser timezone", async () => {
    process.env.TZ = "UTC";
    const page = await ImportOverviewPage();
    process.env.TZ = "Asia/Shanghai";
    render(page);
    expect(screen.getByRole("heading", { name: "2026年10月2日 03:35 · NES 游戏" })).toBeInTheDocument();
  });
});
