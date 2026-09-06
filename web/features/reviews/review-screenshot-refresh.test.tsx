import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ReviewActions, type ReviewWorkspace } from "./review-actions";

vi.mock("next/navigation", () => ({ useRouter: () => ({ refresh: vi.fn() }) }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "user-1" } } }) }));

const review: ReviewWorkspace = {
  itemId: "item-1", version: 1,
  platformInstance: { id: "platform-1", name: "ONS" },
  metadata: { title: "ONS Project", description: "", developer: "", publisher: "", genre: "", players: null, releaseYear: null },
  validation: { id: "validation-1", status: "READY", compatibilityCode: "READY" },
  candidates: [], uploadedAssets: [], scrapeRuns: [], selectedCandidateId: null,
  selectedAssets: { coverCandidateAssetId: null, coverUploadedAssetId: null, backgroundCandidateAssetId: null, screenshotCandidateAssetIds: [] },
  defaultDosEntry: null, dosEntries: [],
};

function screenshotReview(id: string): ReviewWorkspace {
  return { ...review, runtimeScreenshot: {
    screenshotId: id, validationId: "validation-1", providerId: "retrom-runtime", targetId: "onscripter-yuri",
    widthPx: 800, heightPx: 600, capturedAtMs: 1, url: `/api/v1/admin/review-assets/${id}`,
  } };
}

function previewWindow() {
  return {
    closed: false, opener: window,
    document: { title: "", body: { style: {}, textContent: "" } },
    location: { origin: window.location.origin, pathname: "/admin/review-previews/preview-1", replace: vi.fn() },
    close: vi.fn(),
  };
}

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
}

async function notifyScreenshot(source: unknown, data: unknown = {
  type: "retrom-review-screenshot", importItemId: "item-1", previewId: "preview-1",
}, origin = window.location.origin) {
  await act(() => window.dispatchEvent(new MessageEvent("message", {
    origin, source: source as Window, data,
  })));
}

beforeEach(() => sessionStorage.clear());
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("review screenshot refresh notifications", () => {
  it("updates every saved screenshot from the open preview, including after the review remounts", async () => {
    const popup = previewWindow();
    vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);
    let current = review;
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).endsWith("/previews") && init?.method === "POST") {
        return Promise.resolve(jsonResponse({ previewId: "preview-1", playUrl: "/admin/review-previews/preview-1" }, 201));
      }
      expect(String(input)).toBe("/api/v1/admin/reviews/item-1");
      expect(init).toEqual({ cache: "no-store" });
      return Promise.resolve(jsonResponse(current));
    }));
    const mounted = render(<ReviewActions review={review} />);
    await userEvent.setup().click(screen.getByRole("button", { name: "运行游戏" }));
    await waitFor(() => expect(popup.location.replace).toHaveBeenCalledOnce());
    current = screenshotReview("screenshot-1");
    await notifyScreenshot(popup);
    expect(screen.getByRole("img", { name: "ONS Project 的运行截图" })).toHaveAttribute("src", current.runtimeScreenshot?.url);

    // A browser refresh discards the hook's Window map while the game stays open.
    mounted.unmount();
    render(<ReviewActions review={current} />);
    for (const id of ["screenshot-2", "screenshot-3"]) {
      current = screenshotReview(id);
      await notifyScreenshot(popup);
      expect(screen.getByRole("img", { name: "ONS Project 的运行截图" })).toHaveAttribute("src", current.runtimeScreenshot?.url);
      expect(screen.getByText("已更新运行截图")).toBeVisible();
      expect(screen.getByRole("button", { name: "从试玩存档继续" })).toBeDisabled();
    }
  });

  it("ignores unrelated origins, items, messages and source windows", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    render(<ReviewActions review={review} />);
    const popup = previewWindow();
    await notifyScreenshot(popup, undefined, "https://untrusted.example");
    for (const source of [
      null, window, { ...popup, opener: null }, { ...popup, closed: true },
      { ...popup, location: { ...popup.location, pathname: "/admin/review-previews/another-preview" } },
      { ...popup, location: { ...popup.location, origin: "https://untrusted.example" } },
      { ...popup, get location() { throw new DOMException("Cross-origin access", "SecurityError"); } },
    ]) {
      await notifyScreenshot(source);
    }
    for (const data of [null, {}, {
      type: "retrom-review-screenshot", importItemId: "another-item", previewId: "preview-1",
    }, { type: "retrom-review-screenshot", importItemId: "item-1", previewId: "" }, {
      type: "retrom-review-checkpoint", previewId: "preview-1",
    }]) {
      await notifyScreenshot(popup, data);
    }
    expect(fetchMock).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "从试玩存档继续" })).toBeDisabled();
  });

  it("keeps the last screenshot and shows a warning when the authoritative refresh fails", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse({}, 500)));
    const current = screenshotReview("screenshot-1");
    render(<ReviewActions review={current} />);
    await notifyScreenshot(previewWindow());
    expect(screen.getByText("截图已保存，但审核页刷新失败")).toBeVisible();
    expect(screen.getByRole("img", { name: "ONS Project 的运行截图" })).toHaveAttribute("src", current.runtimeScreenshot?.url);
  });
});
