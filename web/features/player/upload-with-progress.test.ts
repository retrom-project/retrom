import { describe, expect, it, vi } from "vitest";
import { uploadWithProgress, uploadWithRestartRetry } from "./upload-with-progress";

class EventTargetStub {
  private readonly listeners = new Map<string, Array<(event: ProgressEvent) => void>>();
  addEventListener(name: string, listener: (event: ProgressEvent) => void) {
    this.listeners.set(name, [...(this.listeners.get(name) ?? []), listener]);
  }
  emit(name: string, event = new ProgressEvent(name)) {
    for (const listener of this.listeners.get(name) ?? []) {listener(event);}
  }
}

class RequestStub extends EventTargetStub {
  readonly upload = new EventTargetStub();
  status = 0;
  responseText = "";
  timeout = 0;
  withCredentials = false;
  readonly open = vi.fn();
  readonly setRequestHeader = vi.fn();
  readonly send = vi.fn();
}

describe("uploadWithProgress", () => {
  it("reports real upload bytes and resolves only after the server response", async () => {
    const xhr = new RequestStub();
    const onProgress = vi.fn();
    const result = uploadWithProgress({
      method: "PUT", url: "/save", body: Uint8Array.of(1, 2, 3, 4), totalBytes: 4,
      headers: { "X-Test": "yes" }, onProgress,
      createRequest: () => xhr as unknown as XMLHttpRequest,
    });
    xhr.upload.emit("loadstart");
    xhr.upload.emit("progress", new ProgressEvent("progress", { lengthComputable: true, loaded: 2, total: 4 }));
    expect(onProgress).toHaveBeenLastCalledWith({ loaded: 2, total: 4, percent: 50 });
    xhr.status = 204;
    xhr.emit("load");
    await expect(result).resolves.toEqual({ ok: true, status: 204, body: "" });
    expect(onProgress).toHaveBeenLastCalledWith({ loaded: 4, total: 4, percent: 100 });
    expect(xhr.withCredentials).toBe(true);
    expect(xhr.timeout).toBe(120_000);
    expect(xhr.setRequestHeader).toHaveBeenCalledWith("X-Test", "yes");
  });

  it("ends a failed HTTP upload normally but rejects a network failure", async () => {
    const failedResponse = new RequestStub();
    const response = uploadWithProgress({
      method: "POST", url: "/save", body: new FormData(), totalBytes: 8, onProgress: vi.fn(),
      createRequest: () => failedResponse as unknown as XMLHttpRequest,
    });
    failedResponse.status = 409;
    failedResponse.emit("load");
    await expect(response).resolves.toEqual({ ok: false, status: 409, body: "" });

    const networkFailure = new RequestStub();
    const rejected = uploadWithProgress({
      method: "PUT", url: "/save", body: Uint8Array.of(1), onProgress: vi.fn(),
      createRequest: () => networkFailure as unknown as XMLHttpRequest,
    });
    networkFailure.emit("error");
    await expect(rejected).rejects.toThrow("SAVE_UPLOAD_NETWORK_FAILED");

    const timedOut = new RequestStub();
    const timeout = uploadWithProgress({
      method: "POST", url: "/save", body: new FormData(), timeoutMs: 300_000, onProgress: vi.fn(),
      createRequest: () => timedOut as unknown as XMLHttpRequest,
    });
    expect(timedOut.timeout).toBe(300_000);
    timedOut.emit("timeout");
    await expect(timeout).rejects.toThrow("SAVE_UPLOAD_TIMEOUT");
  });
});

describe("uploadWithRestartRetry", () => {
  const request = () => ({
    method: "POST" as const, url: "/save", body: new FormData(),
    headers: {"Idempotency-Key": "stable-key"}, onProgress: vi.fn(),
  });

  it("retries a network loss and temporary response with the same idempotency key", async () => {
    const send = vi.fn().mockRejectedValueOnce(new Error("server restarted"))
      .mockResolvedValueOnce({ok: false, status: 503, body: ""})
      .mockResolvedValueOnce({ok: true, status: 201, body: "{}"});
    const wait = vi.fn().mockResolvedValue(undefined);
    const input = request();
    await expect(uploadWithRestartRetry(input, send, wait)).resolves.toMatchObject({status: 201});
    expect(send).toHaveBeenCalledTimes(3);
    expect(send.mock.calls.every(([actual]) => actual === input)).toBe(true);
    expect(wait.mock.calls).toEqual([[1000], [2000]]);
  });

  it("does not retry a conflict or invalid upload", async () => {
    const send = vi.fn().mockResolvedValue({ok: false, status: 409, body: "conflict"});
    await expect(uploadWithRestartRetry(request(), send, vi.fn())).resolves.toMatchObject({status: 409});
    expect(send).toHaveBeenCalledTimes(1);
  });
});
