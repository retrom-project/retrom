import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { configureAuthenticatedClient } from "@/lib/api/client";
import { refreshReviewQueue } from "./review-queue-refresh";
import { ReviewDeduplicate } from "./review-deduplicate";

vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "admin" } } }) }));
vi.mock("./review-queue-refresh", () => ({ refreshReviewQueue: vi.fn() }));
const after = "01990000-0000-7000-8000-000000000001";
const through = "01990000-0000-7000-8000-000000000002";

describe("ReviewDeduplicate", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    vi.unstubAllGlobals();
    configureAuthenticatedClient({ csrfToken: null, onAuthenticationFailure: null });
  });

  it("automatically traverses the complete filter scope and reports skipped attachments", async () => {
    const requests: Request[] = [];
    configureAuthenticatedClient({ csrfToken: "csrf-test", onAuthenticationFailure: null });
    vi.stubGlobal("fetch", vi.fn(async (request: Request) => {
      requests.push(request.clone());
      return Response.json({ scannedCount: 50, discardedCount: 2, attachmentActiveCount: 1,
        nextAfterItemId: requests.length === 1 ? after : null, throughItemId: through });
    }));
    render(<ReviewDeduplicate values={{ pegasusImportId: through, q: "test", sort: "UPDATED_DESC", cursor: "ignored" }} />);
    expect(requests).toHaveLength(0);
    await userEvent.setup().click(screen.getByRole("button", { name: "快速去重" }));
    await waitFor(() => expect(refreshReviewQueue).toHaveBeenCalledWith("admin", {
      tone: "warn", message: "去重完成，已丢弃 4 个与已发布游戏重复的条目。另有 2 个条目正在补传，已跳过。",
    }));
    expect(requests).toHaveLength(2);
    expect(await requests[0].json()).toEqual({ scope: { pegasusImportId: through, q: "test" } });
    expect(await requests[1].json()).toEqual({ scope: { pegasusImportId: through, q: "test" }, afterItemId: after, throughItemId: through });
    expect(requests[0].headers.get("X-Retrom-Csrf")).toBe("csrf-test");
    expect(requests[0].headers.get("Idempotency-Key")).toBeTruthy();
    expect(requests[0].headers.get("Idempotency-Key")).not.toBe(requests[1].headers.get("Idempotency-Key"));
  });

  it("refreshes committed results and explains retry after a later page fails", async () => {
    let calls = 0;
    vi.stubGlobal("fetch", vi.fn(async () => {
      calls++;
      if (calls === 2) { throw new Error("连接中断"); }
      return Response.json({ scannedCount: 50, discardedCount: 3, attachmentActiveCount: 0, nextAfterItemId: after, throughItemId: through });
    }));
    render(<ReviewDeduplicate values={{}} />);
    await userEvent.setup().click(screen.getByRole("button", { name: "快速去重" }));
    await waitFor(() => expect(refreshReviewQueue).toHaveBeenCalledWith("admin", {
      tone: "bad", message: "连接中断；已确认丢弃 3 个条目，可再次点击快速去重继续处理。",
    }));
    expect(calls).toBe(2);
  });

  it("disables repeated clicks while running and handles no duplicates", async () => {
    let resolve: (value: Response) => void = () => undefined;
    const pending = new Promise<Response>((done) => { resolve = done; });
    const fetch = vi.fn(() => pending);
    vi.stubGlobal("fetch", fetch);
    render(<ReviewDeduplicate values={{}} />);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "快速去重" }));
    const busy = screen.getByRole("button", { name: "正在去重…" });
    expect(busy).toBeDisabled();
    await user.click(busy);
    expect(fetch).toHaveBeenCalledTimes(1);
    resolve(Response.json({ scannedCount: 0, discardedCount: 0, attachmentActiveCount: 0, nextAfterItemId: null, throughItemId: null }));
    await waitFor(() => expect(refreshReviewQueue).toHaveBeenCalledWith("admin", {
      tone: "good", message: "去重完成，已丢弃 0 个与已发布游戏重复的条目。",
    }));
  });
});
