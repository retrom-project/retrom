import type * as ClientModule from "@/lib/api/client";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api, ApiError, upload } from "@/lib/api/client";
import { checkpoint, runFixture } from "./player-test-fixture";
import { makeDraft, uploadDraft } from "./save-drafts";
import {
  DraftIdentityMismatch,
  sameIdentity,
  uploadWithRenewal,
} from "./draft-upload";

vi.mock("@/lib/api/client", async () => {
  const actual = await vi.importActual<typeof ClientModule>("@/lib/api/client");
  return {
    ...actual,
    upload: vi.fn(),
    api: { POST: vi.fn(), DELETE: vi.fn() },
  };
});

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.DELETE).mockResolvedValue({
    data: undefined,
    response: new Response(null, { status: 204 }),
  } as Awaited<ReturnType<typeof api.DELETE>>);
});

function draftFixture() {
  return makeDraft("owner", runFixture(), checkpoint, null, null, true);
}

describe("draft context renewal", () => {
  it("renews an expired context while preserving commit, identity and expected version", async () => {
    const draft = draftFixture();
    draft.saveId = "selected-save";
    draft.metadata.version = 7;
    const original = structuredClone(draft.metadata);
    const renewed = {
      ...runFixture(),
      id: "renewed-run",
      save: { version: 99 },
    };
    vi.mocked(api.POST).mockResolvedValue({
      data: renewed,
      response: new Response(),
    } as Awaited<ReturnType<typeof api.POST>>);
    vi.mocked(upload).mockRejectedValueOnce(
      new ApiError("CONTEXT_EXPIRED", "expired", 409),
    );
    vi.mocked(upload).mockResolvedValueOnce({ id: draft.saveId, version: 8 });

    await uploadWithRenewal(draft);

    expect(api.POST).toHaveBeenCalledWith("/api/v1/runs", {
      body: {
        gameId: draft.gameId,
        purpose: "play",
        coreId: original.extinfo.coreId,
        saveId: "selected-save",
      },
    });
    const body = vi.mocked(upload).mock.calls[1][1];
    expect(JSON.parse(String(body.get("metadata")))).toEqual({
      ...original,
      runId: "renewed-run",
    });
    expect(draft.metadata).toEqual(original);
    expect(api.DELETE).toHaveBeenCalledWith("/api/v1/runs/{runId}", {
      params: { path: { runId: "renewed-run" } },
    });
  });

  it.each([
    "romHash",
    "coreFingerprint",
    "coreId",
    "checkpointFormat",
  ] as const)("blocks changed %s without rewriting the draft", async (key) => {
    const draft = draftFixture();
    const original = structuredClone(draft.metadata);
    const renewed = runFixture();
    renewed.extinfo[key] = "changed";
    vi.mocked(api.POST).mockResolvedValue({
      data: renewed,
      response: new Response(),
    } as Awaited<ReturnType<typeof api.POST>>);
    vi.mocked(upload).mockRejectedValueOnce(
      new ApiError("CONTEXT_EXPIRED", "expired", 409),
    );

    await expect(uploadWithRenewal(draft)).rejects.toBeInstanceOf(
      DraftIdentityMismatch,
    );

    expect(upload).toHaveBeenCalledOnce();
    expect(draft.metadata).toEqual(original);
    expect(api.DELETE).toHaveBeenCalledOnce();
  });

  it("preserves a failed renewed commit and closes its temporary run", async () => {
    const draft = draftFixture();
    vi.mocked(api.POST).mockResolvedValue({
      data: runFixture(),
      response: new Response(),
    } as Awaited<ReturnType<typeof api.POST>>);
    vi.mocked(upload)
      .mockRejectedValueOnce(new ApiError("CONTEXT_EXPIRED", "expired", 409))
      .mockRejectedValueOnce(new TypeError("offline"));
    await expect(uploadWithRenewal(draft)).rejects.toThrow("offline");
    expect(api.DELETE).toHaveBeenCalledOnce();
    expect(draft.metadata.runId).toBe(runFixture().id);
  });

  it("explains a version conflict while retaining the frozen draft and never renewing its version", async () => {
    const draft = draftFixture();
    draft.saveId = "selected-save";
    draft.metadata.version = 7;
    const original = structuredClone(draft.metadata);
    vi.mocked(upload).mockRejectedValue(
      new ApiError(
        "VERSION_CONFLICT",
        "Item changed; refresh and try again",
        409,
      ),
    );
    await expect(uploadWithRenewal(draft)).rejects.toThrow("另一个页面");
    await expect(uploadWithRenewal(draft)).rejects.toThrow("不会强行覆盖");
    expect(draft.metadata).toEqual(original);
    expect(api.POST).not.toHaveBeenCalled();
    for (const [, body] of vi.mocked(upload).mock.calls) {
      expect(JSON.parse(String(body.get("metadata")))).toEqual(original);
    }
  });

  it("does not create a new context for a disconnected upload", async () => {
    vi.mocked(upload).mockRejectedValueOnce(new TypeError("offline"));
    await expect(uploadWithRenewal(draftFixture())).rejects.toThrow("offline");
    expect(api.POST).not.toHaveBeenCalled();
  });

  it("rejects another owner's draft before contacting the server", async () => {
    await expect(uploadDraft(draftFixture(), "other-user")).rejects.toThrow(
      "其他账号",
    );
    expect(upload).not.toHaveBeenCalled();
    expect(api.POST).not.toHaveBeenCalled();
  });

  it("compares nested options and content structurally, without depending on key order", () => {
    expect(
      sameIdentity(
        { a: [1, { b: true }], c: null },
        { c: null, a: [1, { b: true }] },
      ),
    ).toBe(true);
    expect(
      sameIdentity(
        { options: { gameId: "old" } },
        { options: { gameId: "new" } },
      ),
    ).toBe(false);
    expect(
      sameIdentity(
        { content: { entryFile: "old" } },
        { content: { entryFile: "new" } },
      ),
    ).toBe(false);
  });
});
