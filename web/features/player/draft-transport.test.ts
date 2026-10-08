import { afterEach, describe, expect, it, vi } from "vitest";
import { configureClient } from "@/lib/api/client";
import { checkpoint, runFixture } from "./player-test-fixture";
import { uploadWithRenewal } from "./draft-upload";
import { makeDraft } from "./save-drafts";

afterEach(() => {
  configureClient("");
  vi.unstubAllGlobals();
});

describe("save multipart transport", () => {
  it("keeps a provider JPEG screenshot's media type, bytes and filename in the actual multipart request", async () => {
    const jpeg = new Blob([Uint8Array.of(255, 216, 255, 217)], {
      type: "image/jpeg",
    });
    const draft = makeDraft(
      "owner",
      runFixture(),
      checkpoint,
      jpeg,
      null,
      false,
    );
    const fetchRequest = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ id: draft.id, version: 1 }), {
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchRequest);
    await uploadWithRenewal(draft);
    const body = fetchRequest.mock.calls[0][1]?.body;
    if (!(body instanceof FormData)) {
      throw new Error("Expected multipart form data.");
    }
    const screenshot = body.get("screenshot");
    if (!(screenshot instanceof File)) {
      throw new Error("Expected the original screenshot file.");
    }
    expect(screenshot.name).toBe("screenshot.jpg");
    expect(screenshot.type).toBe("image/jpeg");
    expect(await screenshot.arrayBuffer()).toEqual(await jpeg.arrayBuffer());
  });
  it("overwrites through the canonical PUT route with the frozen CAS metadata", async () => {
    const draft = makeDraft(
      "owner",
      runFixture(),
      checkpoint,
      null,
      null,
      true,
    );
    draft.saveId = "existing-save";
    draft.metadata.version = 7;
    const original = structuredClone(draft.metadata);
    const fetchRequest = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ id: draft.saveId, version: 8 }), {
        headers: { "Content-Type": "application/json" },
      }),
    );
    configureClient("current-csrf");
    vi.stubGlobal("fetch", fetchRequest);

    const saved = await uploadWithRenewal(draft);

    expect(saved.version).toBe(8);
    expect(fetchRequest).toHaveBeenCalledOnce();
    const [path, init] = fetchRequest.mock.calls[0];
    expect(path).toBe(`/api/v1/saves/${draft.saveId}`);
    expect(init?.method).toBe("PUT");
    expect(init?.headers).toEqual({ "X-Retrom-Csrf": "current-csrf" });
    const body = init?.body;
    expect(body).toBeInstanceOf(FormData);
    if (!(body instanceof FormData)) {
      throw new Error("The save must use multipart FormData.");
    }
    expect(JSON.parse(String(body.get("metadata")))).toEqual(original);
    expect(body.get("payload")).toBeInstanceOf(Blob);
    expect(draft.metadata).toEqual(original);
  });
});
