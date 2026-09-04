import {describe, expect, it, vi} from "vitest";

import type {LaunchEnvelopeV1, PlayerRuntimeV1, RuntimeHostV1} from "./contract";
import {loadProviderRuntime} from "./provider-dispatcher";

describe("Provider Module V1 dispatcher", () => {
  it("loads only the exact module URL and verifies exported identity before creation", async () => {
    const envelope = fixtureEnvelope();
    const runtime = fixtureRuntime();
    const validateLaunchRequest = vi.fn((value) => value);
    const createRuntime = vi.fn(async () => runtime);
    const importer = vi.fn(async () => ({
      createRuntime,
      providerApiVersion: 1,
      providerId: "fixture",
      providerVersion: "1.0.0",
      validateLaunchRequest,
    }));
    const host = fixtureHost();
    const environment = verifiedEnvironment(envelope);
    await expect(loadProviderRuntime(envelope, host, importer, environment)).resolves.toBe(runtime);
    expect(environment.fetcher).toHaveBeenCalledWith(envelope.runtime.moduleUrl, {
      cache: "no-store", credentials: "same-origin", redirect: "error", signal: host.signal,
    });
    expect(importer).toHaveBeenCalledWith("blob:retrom-provider");
    expect(environment.revokeModuleUrl).toHaveBeenCalledWith("blob:retrom-provider");
    expect(validateLaunchRequest).toHaveBeenCalledWith(envelope);
    expect(createRuntime).toHaveBeenCalledWith(envelope, host);
  });

  it("invokes the default browser fetch with the global receiver", async () => {
    const envelope = fixtureEnvelope();
    const runtime = fixtureRuntime();
    const originalFetch = globalThis.fetch;
    const browserFetch = vi.fn(function(this: unknown) {
      if (this !== globalThis) {throw new TypeError("Illegal invocation");}
      return Promise.resolve(new Response("export{}", {
        headers: {"content-length": "8", "content-type": "text/javascript; charset=utf-8"},
      }));
    });
    vi.stubGlobal("fetch", browserFetch);
    try {
      await expect(loadProviderRuntime(envelope, fixtureHost(), async () => ({
        createRuntime: vi.fn(async () => runtime),
        providerApiVersion: 1,
        providerId: "fixture",
        providerVersion: "1.0.0",
        validateLaunchRequest: vi.fn((value) => value),
      }), {
        createModuleUrl: vi.fn(() => "blob:retrom-provider"),
        crossOriginIsolated: true,
        revokeModuleUrl: vi.fn(),
        sha256: vi.fn(async () => envelope.runtime.moduleSha256),
      })).resolves.toBe(runtime);
    } finally {
      vi.stubGlobal("fetch", originalFetch);
    }
    expect(browserFetch).toHaveBeenCalledOnce();
  });

  it("rejects identity mismatches and modules with extra exports", async () => {
    const envelope = fixtureEnvelope();
    const base = {
      createRuntime: vi.fn(async () => fixtureRuntime()),
      providerApiVersion: 1,
      providerId: "other",
      providerVersion: "1.0.0",
      validateLaunchRequest: vi.fn((value) => value),
    };
    await expect(loadProviderRuntime(envelope, fixtureHost(), async () => base, verifiedEnvironment(envelope)))
      .rejects.toThrow("PLAYER_PROVIDER_MODULE_INVALID");
    await expect(loadProviderRuntime(envelope, fixtureHost(), async () => ({
      ...base, providerId: "fixture", debugAdapter: "leaked",
    }), verifiedEnvironment(envelope))).rejects.toThrow("PLAYER_PROVIDER_MODULE_INVALID");
  });

  it("fails before import when a threaded target is not cross-origin isolated", async () => {
    const envelope = fixtureEnvelope();
    envelope.runtime.capabilities.requiresThreads = true;
    const importer = vi.fn(async () => {throw new Error("must not import");});
    await expect(loadProviderRuntime(envelope, fixtureHost(), importer, {
      ...verifiedEnvironment(envelope), crossOriginIsolated: false,
    }))
      .rejects.toThrow("PLAYER_RUNTIME_THREADS_UNAVAILABLE");
    expect(importer).not.toHaveBeenCalled();
  });

  it("rejects a runtime whose initial state or capabilities differ from the target contract", async () => {
    const envelope = fixtureEnvelope();
    const providerModule = (runtime: PlayerRuntimeV1) => ({
      createRuntime: vi.fn(async () => runtime), providerApiVersion: 1 as const, providerId: "fixture",
      providerVersion: "1.0.0", validateLaunchRequest: vi.fn(() => envelope),
    });
    const wrongState = fixtureRuntime();
    wrongState.getState = () => "RUNNING";
    await expect(loadProviderRuntime(envelope, fixtureHost(), async () => providerModule(wrongState),
      verifiedEnvironment(envelope)))
      .rejects.toThrow("PLAYER_PROVIDER_MODULE_INVALID");
    const wrongCapabilities = fixtureRuntime();
    wrongCapabilities.getCapabilities = () => ({...envelope.runtime.capabilities, pause: true});
    await expect(loadProviderRuntime(envelope, fixtureHost(), async () => providerModule(wrongCapabilities),
      verifiedEnvironment(envelope)))
      .rejects.toThrow("PLAYER_PROVIDER_MODULE_INVALID");
  });

  it("rejects unknown or malformed nested Envelope fields before import", async () => {
    const cases: LaunchEnvelopeV1[] = [];
    for (const mutate of [
      (value: LaunchEnvelopeV1) => Object.assign(value.session, {adapterId: "leaked"}),
      (value: LaunchEnvelopeV1) => Object.assign(value.runtime.capabilities, {extra: true}),
      (value: LaunchEnvelopeV1) => {value.session.returnTo = "https://evil.example";},
      (value: LaunchEnvelopeV1) => {value.runtime.checkpoint = {maxBytes: 0, readFormats: ["bad"], writeFormat: "bad"};},
      (value: LaunchEnvelopeV1) => {value.restore = {
        format: "fixture-v1", sha256: "a".repeat(64), sizeBytes: 1, url: "https://evil.example/state",
      };},
    ]) {
      const candidate = structuredClone(fixtureEnvelope());
      mutate(candidate);
      cases.push(candidate);
    }
    const importer = vi.fn(async () => {throw new Error("must not import");});
    for (const candidate of cases) {
      await expect(loadProviderRuntime(candidate, fixtureHost(), importer, verifiedEnvironment(candidate)))
        .rejects.toThrow("PLAYER_LAUNCH_ENVELOPE_INVALID");
    }
    expect(importer).not.toHaveBeenCalled();
  });

  it("leaves exact targetOptions validation to the Provider module", async () => {
    const envelope = fixtureEnvelope();
    Object.assign(envelope.targetOptions, {providerOwnedOption: true});
    const validateLaunchRequest = vi.fn(() => {throw new Error("PROVIDER_LAUNCH_REQUEST_INVALID");});
    const importer = vi.fn(async () => ({
      createRuntime: vi.fn(), providerApiVersion: 1, providerId: "fixture",
      providerVersion: "1.0.0", validateLaunchRequest,
    }));
    await expect(loadProviderRuntime(envelope, fixtureHost(), importer, verifiedEnvironment(envelope)))
      .rejects.toThrow("PROVIDER_LAUNCH_REQUEST_INVALID");
    expect(importer).toHaveBeenCalledOnce();
    expect(validateLaunchRequest).toHaveBeenCalledWith(envelope);
  });

  it("rejects oversized, mislabeled and digest-mismatched module bytes before import", async () => {
    const envelope = fixtureEnvelope();
    const importer = vi.fn();
    const cases: Array<[Response, string]> = [
      [new Response("module", {headers: {"content-type": "text/plain"}}), "PLAYER_PROVIDER_MODULE_INVALID"],
      [new Response("module", {headers: {"content-length": String(8 * 1024 * 1024 + 1),
        "content-type": "text/javascript; charset=utf-8"}}), "PLAYER_PROVIDER_MODULE_INVALID"],
      [new Response("module", {headers: {"content-type": "text/javascript; charset=utf-8"}}),
        "PLAYER_PROVIDER_MODULE_DIGEST_INVALID"],
    ];
    for (const [response, expectedCode] of cases) {
      const environment = verifiedEnvironment(envelope);
      environment.fetcher.mockResolvedValueOnce(response as Response);
      if (response.headers.get("content-type")?.startsWith("text/javascript") &&
        response.headers.get("content-length") === null) {environment.sha256.mockResolvedValueOnce("f".repeat(64));}
      await expect(loadProviderRuntime(envelope, fixtureHost(), importer, environment))
        .rejects.toMatchObject({code: expectedCode});
    }
    expect(importer).not.toHaveBeenCalled();
  });
});

function verifiedEnvironment(envelope: LaunchEnvelopeV1) {
  return {
    createModuleUrl: vi.fn(() => "blob:retrom-provider"),
    crossOriginIsolated: true,
    fetcher: vi.fn(async () => new Response("export{}", {
      headers: {"content-length": "8", "content-type": "text/javascript; charset=utf-8"},
    })),
    revokeModuleUrl: vi.fn(),
    sha256: vi.fn(async () => envelope.runtime.moduleSha256),
  };
}

function fixtureEnvelope(): LaunchEnvelopeV1 {
  const digest = "a".repeat(64);
  const bundle = "b".repeat(64);
  return {
    resources: [],
    restore: null,
    runtime: {
      bundleSha256: bundle,
      capabilities: {
        checkpoint: false, frameCounter: false, frameMode: "NONE", pause: false,
        discSwitch: false, inputFilter: false, nativeSettings: false, netplayPort: false,
        requiresThreads: false, screenshot: false, standardGamepad: false,
        validationProbes: [], videoModes: [], volume: false,
      },
      checkpoint: null,
      gameCompatibilityLine: "fixture-v1",
      moduleSha256: digest,
      moduleUrl: `/runtime/providers/fixture/${bundle}/client.mjs`,
      providerApiVersion: 1,
      providerId: "fixture",
      providerVersion: "1.0.0",
      runtimeBaseUrl: `/runtime/providers/fixture/${bundle}/`,
      targetContractSha256: digest,
      targetId: "fixture",
    },
    netplay: null,
    schemaVersion: 1,
    session: {
      coreName: "Fixture Core",
      id: "018f0f31-26fe-7a31-9d61-4ec92f16d4c3", mode: "SINGLE",
      platformName: "Fixture", purpose: "PRODUCT", returnTo: "/games/fixture",
      title: "Fixture", warnings: [],
    },
    targetOptions: {},
    validation: null,
  } as unknown as LaunchEnvelopeV1;
}

function fixtureHost(): RuntimeHostV1 {
  return {
    loadRestore: vi.fn(async () => null),
    mountFrame: vi.fn(async () => {throw new Error("unused");}),
    reportDiagnostic: vi.fn(),
    signal: new AbortController().signal,
  };
}

function fixtureRuntime(): PlayerRuntimeV1 {
  return {
    checkpoint: vi.fn(async () => {throw new Error("unused");}),
    closeNativeSettings: vi.fn(async () => {throw new Error("unused");}),
    exit: vi.fn(async () => undefined),
    getCanvas: () => null,
    getCapabilities: () => fixtureEnvelope().runtime.capabilities,
    getCheckpointAvailability: () => ({available: false, reason: "UNSUPPORTED"}),
    getDiscState: vi.fn(async () => {throw new Error("unused");}),
    getFrameCount: () => null,
    getNetplayPort: vi.fn(async () => {throw new Error("unused");}),
    getState: () => "CREATED",
    mount: vi.fn(async () => undefined),
    openNativeSettings: vi.fn(async () => {throw new Error("unused");}),
    pause: vi.fn(async () => undefined),
    resume: vi.fn(async () => undefined),
    runValidationProbe: vi.fn(async () => {throw new Error("unused");}),
    screenshot: vi.fn(async () => new Blob()),
    setInputFilter: vi.fn(async () => {throw new Error("unused");}),
    setVideoMode: vi.fn(async () => {throw new Error("unused");}),
    setVolume: vi.fn(async () => undefined),
    subscribe: () => () => undefined,
    switchDisc: vi.fn(async () => {throw new Error("unused");}),
  };
}
