import {afterEach, expect, it, vi} from "vitest";
import type {GameSaveDraft} from "./game-save-draft-store";
import {uploadLocalGameSave} from "./local-game-save-upload";

vi.mock("@/lib/api/client", () => ({
  writeHeaders: (headers: Record<string, string>) => ({...headers, "X-Retrom-Csrf": "current-session"}),
  handleAuthenticationResponse: (response: Response) => response,
}));
vi.mock(import("./manual-save-screenshot"), async (original) => ({...await original(),
  prepareManualSaveScreenshot: async (image: {screenshot: Blob}) => image.screenshot.size ? {...image, format: "png"} : null,
}));

const draft: GameSaveDraft = {userId: "owner", launchId: "original-launch", title: "游戏", restored: false, updatedAtMs: 1,
  payload: {source: "GAME_SAVE", requestId: "stable-request", name: "首次暂存时的名称", screenshot: new Blob(["image"], {type: "image/png"}),
    checkpoint: {bytes: Uint8Array.of(1, 2), format: "j2me-rms-bundle-v1", metadata: null}}};
afterEach(() => {vi.unstubAllGlobals();});

it("recovers through the owned launch using current account authentication and the original idempotency key", async () => {
  const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response("{}", {status: 201})); vi.stubGlobal("fetch", fetcher);
  await uploadLocalGameSave(draft); await uploadLocalGameSave(draft);
  expect(fetcher).toHaveBeenCalledWith("/api/v1/launches/original-launch/local-save", expect.objectContaining({
    credentials: "same-origin", method: "POST", headers: {"Idempotency-Key": "stable-request", "X-Retrom-Csrf": "current-session"},
  }));
  const forms = fetcher.mock.calls.map((call) => call[1]?.body);
  for (const form of forms) {
    expect(form).toBeInstanceOf(FormData);
    if (!(form instanceof FormData)) {throw Error("Expected multipart save");}
    expect(form.get("payload")).toBeInstanceOf(Blob);
    expect(form.get("screenshot")).toBeInstanceOf(Blob);
  }
});

it.each([409, 503])("rejects %s without treating an uncommitted draft as saved", async (status) => {
  vi.stubGlobal("fetch", vi.fn<typeof fetch>().mockResolvedValue(new Response("{}", {status})));
  await expect(uploadLocalGameSave(draft)).rejects.toThrow(status === 409 ? "原存档已更新或删除" : "本地草稿已保留");
});


it("recovers a final native save without a screenshot", async () => {
  const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response("{}", {status: 201})); vi.stubGlobal("fetch", fetcher);
  await uploadLocalGameSave({...draft, payload: {...draft.payload, screenshot: new Blob()}});
  const body = fetcher.mock.calls[0][1]!.body as FormData;
  expect(body.has("payload")).toBe(true); expect(body.has("screenshot")).toBe(false);
});
