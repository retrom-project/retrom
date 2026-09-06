import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ReviewActions, type ReviewWorkspace } from "./review-actions";

vi.mock("next/navigation", () => ({ useRouter: () => ({ refresh: vi.fn() }) }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "user-1" } } }) }));

const review: ReviewWorkspace = {
  itemId: "item-1", version: 1,
  platformInstance: { id: "platform-1", name: "Arcade" },
  metadata: { title: "Trial", description: "", developer: "", publisher: "", genre: "", players: null, releaseYear: null },
  validation: { id: "validation-1", status: "READY", compatibilityCode: "READY" },
  candidates: [], uploadedAssets: [], scrapeRuns: [], selectedCandidateId: null,
  selectedAssets: { coverCandidateAssetId: null, coverUploadedAssetId: null, backgroundCandidateAssetId: null, screenshotCandidateAssetIds: [] },
  defaultDosEntry: null, dosEntries: [],
};

function previewWindow() {
  return { closed: false, opener: window, location: {
    origin: window.location.origin, pathname: "/admin/review-previews/preview-1",
  } };
}

const checkpoint = { type: "retrom-review-checkpoint", importItemId: "item-1", previewId: "preview-1" };

async function notify(source: unknown, data: unknown = checkpoint, origin = window.location.origin) {
  await act(() => window.dispatchEvent(new MessageEvent("message", { origin, source: source as Window, data })));
}

beforeEach(() => sessionStorage.clear());
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("review checkpoint notifications", () => {
  it("accepts a newly saved checkpoint from the still-open game after the review remounts", async () => {
    const popup = previewWindow();
    const first = render(<ReviewActions review={review} />);
    first.unmount();
    render(<ReviewActions review={review} />);
    const restore = screen.getByRole("button", { name: "从试玩存档继续" });
    expect(restore).toBeDisabled();
    await notify(popup);
    expect(restore).toBeEnabled();
    expect(screen.getByText("试玩存档已保存，可打开新的游戏窗口继续；临时存档到期或审核结束后释放。")).toBeVisible();
    await notify(popup);
    expect(restore).toBeEnabled();
  });

  it("ignores unrelated or unverifiable checkpoint messages", async () => {
    render(<ReviewActions review={review} />);
    const popup = previewWindow();
    await notify(popup, checkpoint, "https://untrusted.example");
    for (const source of [
      null, window, { ...popup, opener: null }, { ...popup, closed: true },
      { ...popup, location: { ...popup.location, pathname: "/admin/review-previews/another-preview" } },
      { ...popup, get location() { throw new DOMException("Cross-origin access", "SecurityError"); } },
    ]) {await notify(source);}
    for (const data of [null, {}, { ...checkpoint, importItemId: "another-item" }, {
      ...checkpoint, previewId: "another-preview",
    }, { ...checkpoint, previewId: "" }, { ...checkpoint, importItemId: undefined }]) {
      await notify(popup, data);
    }
    expect(screen.getByRole("button", { name: "从试玩存档继续" })).toBeDisabled();
  });
});
