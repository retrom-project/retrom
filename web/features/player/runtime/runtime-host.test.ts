import {afterEach, describe, expect, it, vi} from "vitest";

import type {LaunchEnvelopeV1} from "./contract";
import {createRuntimeHost} from "./runtime-host";

afterEach(() => {document.body.replaceChildren();});

describe("RuntimeHostV1", () => {
  it("mounts the exact host-owned frame mode and removes it on abort", async () => {
    const controller = new AbortController();
    const host = createRuntimeHost(envelope(), controller.signal);
    const target = document.createElement("div");
    document.body.append(target);

    const mounted = await host.mountFrame(target, {resourceRole: null});

    expect(mounted.element.src).toBe("about:blank");
    expect(mounted.origin).toBe(location.origin);
    expect(mounted.element.referrerPolicy).toBe("no-referrer");
    expect(mounted.element.className).toBe("player-frame");
    expect(mounted.element.getAttribute("sandbox"))
      .toBe("allow-downloads allow-pointer-lock allow-same-origin allow-scripts");
    expect(mounted.element.allow).toBe("autoplay; fullscreen; gamepad");
    expect(target.contains(mounted.element)).toBe(true);
    controller.abort();
    expect(target.children).toHaveLength(0);
  });

  it("mounts only ordinal-zero web resources matching the declared frame mode", async () => {
    const input = envelope();
    input.runtime.capabilities.frameMode = "ISOLATED_ORIGIN_RESOURCE";
    input.resources = [{
      bootstrapTicket: "t".repeat(48), cleanupUrl: "https://runtime.example.test/cleanup",
      contentDigest: "c".repeat(64), entryUrl: "https://runtime.example.test/entry",
      kind: "ISOLATED_WEB", ordinal: 0, origin: "https://runtime.example.test", role: "game",
    }];
    const controller = new AbortController();
    const fetcher = vi.fn(async () => new Response(null, {status: 204}));
    const host = createRuntimeHost(input, controller.signal, {fetcher});
    const target = document.createElement("div");
    document.body.append(target);

    const mounted = await host.mountFrame(target, {resourceRole: "game"});

    expect(mounted.element.src).toBe("https://runtime.example.test/entry");
    expect(mounted.origin).toBe("https://runtime.example.test");
    controller.abort();
    expect(fetcher).toHaveBeenCalledWith("https://runtime.example.test/cleanup", {
      credentials: "include", keepalive: true, method: "POST",
    });
    await expect(createRuntimeHost(input, new AbortController().signal)
      .mountFrame(target, {resourceRole: "missing"})).rejects.toThrow("PLAYER_RUNTIME_FRAME_INVALID");
  });

  it("rejects frame mounting when the target contract declares NONE", async () => {
    const input = envelope();
    input.runtime.capabilities.frameMode = "NONE";
    await expect(createRuntimeHost(input, new AbortController().signal)
      .mountFrame(document.createElement("div"), {resourceRole: null}))
      .rejects.toThrow("PLAYER_RUNTIME_FRAME_INVALID");
  });

  it("mounts native web content on its declared isolated origin", async () => {
    const input = envelope();
    input.runtime.capabilities.frameMode = "ISOLATED_ORIGIN_RESOURCE";
    input.resources = [{
      bootstrapTicket: "t".repeat(48), cleanupUrl: null,
      contentDigest: "c".repeat(64), entryUrl: "https://runtime.example.test/entry",
      kind: "NATIVE_WEB", ordinal: 0, origin: "https://runtime.example.test", role: "game",
    }];
    const target = document.createElement("div");
    document.body.append(target);

    const mounted = await createRuntimeHost(input, new AbortController().signal)
      .mountFrame(target, {resourceRole: "game"});

    expect(mounted.element.src).toBe("https://runtime.example.test/entry");
    expect(mounted.origin).toBe("https://runtime.example.test");
  });

  it("loads only same-origin restore bytes with exact size and digest", async () => {
    const bytes = new Uint8Array([1, 2, 3]);
    const fetcher = vi.fn(async () => new Response(bytes));
    const sha256 = vi.fn(async () => "d".repeat(64));
    const host = createRuntimeHost(envelope(), new AbortController().signal, {fetcher, sha256});
    const descriptor = {format: "fixture-v1", sha256: "d".repeat(64), sizeBytes: 3, url: "/restore/1"};

    await expect(host.loadRestore(null)).resolves.toBeNull();
    await expect(host.loadRestore(descriptor)).resolves.toEqual(bytes);
    expect(fetcher).toHaveBeenCalledWith("/restore/1", {
      cache: "no-store", credentials: "same-origin", signal: host.signal,
    });
    expect(sha256).toHaveBeenCalledWith(bytes);

    await expect(host.loadRestore({...descriptor, url: "https://evil.example/restore"}))
      .rejects.toThrow("PLAYER_RUNTIME_RESTORE_INVALID");
    await expect(host.loadRestore({...descriptor, sizeBytes: 2}))
      .rejects.toThrow("PLAYER_RUNTIME_RESTORE_INVALID");
    await expect(host.loadRestore({...descriptor, sha256: "e".repeat(64)}))
      .rejects.toThrow("PLAYER_RUNTIME_RESTORE_INVALID");
    await expect(host.loadRestore({...descriptor, format: "future-v2"}))
      .rejects.toThrow("PLAYER_RUNTIME_RESTORE_INVALID");
  });

  it("reports diagnostics through a closed host callback", () => {
    const report = vi.fn();
    const host = createRuntimeHost(envelope(), new AbortController().signal, {report});
    host.reportDiagnostic({code: "FIXTURE", message: "diagnostic"});
    expect(report).toHaveBeenCalledWith({code: "FIXTURE", message: "diagnostic"});
  });
});

function envelope(): LaunchEnvelopeV1 {
  return {
    netplay: null, resources: [], restore: null, schemaVersion: 1,
    runtime: {
      bundleSha256: "b".repeat(64), capabilities: {
        checkpoint: true, frameCounter: false, frameMode: "SAME_ORIGIN_BLANK", pause: true,
        discSwitch: false, inputFilter: false, nativeSettings: false, netplayPort: false,
        requiresThreads: false, screenshot: true, standardGamepad: true,
        videoModes: [], volume: true,
      }, checkpoint: {maxBytes: 1024, readFormats: ["fixture-v1"], writeFormat: "fixture-v1"},
      moduleSha256: "a".repeat(64), moduleUrl: `/runtime/providers/fixture/${"b".repeat(64)}/client.mjs`,
      providerApiVersion: 1, providerId: "fixture", providerVersion: "1.0.0",
      runtimeBaseUrl: `/runtime/providers/fixture/${"b".repeat(64)}/`,
      targetId: "fixture",
    },
    session: {coreName: "Fixture Core", id: "018f0f31-26fe-7a31-9d61-4ec92f16d4c3", mode: "SINGLE", platformName: "Fixture",
      purpose: "PRODUCT", returnTo: "/games/fixture", title: "Fixture", warnings: []},
    targetOptions: {},
  };
}
