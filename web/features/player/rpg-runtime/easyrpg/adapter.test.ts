import { describe, expect, it, vi } from "vitest";
import type { RpgRuntimeConfig } from "../contract";
import { mountEasyRpg } from "./adapter";

type EasyConfig = RpgRuntimeConfig & {
  adapter: Extract<RpgRuntimeConfig["adapter"], { adapterKind: "EASYRPG_WEB" }>;
};

describe("EasyRPG adapter cleanup", () => {
  it("removes the mount DOM and failed loader before rejecting", async () => {
    const target = document.createElement("div");
    document.body.append(target);
    const mounting = mountEasyRpg(easyConfig(), target, window, null);
    await vi.waitFor(() => expect(document.head.querySelector("script[data-retrom-rpg-runtime=easyrpg]")).not.toBeNull());
    const script = document.head.querySelector<HTMLScriptElement>("script[data-retrom-rpg-runtime=easyrpg]");
    expect(script).not.toBeNull();
    script?.dispatchEvent(new Event("error"));

    await expect(mounting).rejects.toThrow("RPG_RUNTIME_ARTIFACT_UNAVAILABLE");
    expect(target.childElementCount).toBe(0);
    expect(document.head.querySelector("script[data-retrom-rpg-runtime=easyrpg]")).toBeNull();
    target.remove();
  });

  it("rejects a runtime that resolves before its persistent filesystem is ready", async () => {
    const target = document.createElement("div");
    document.body.append(target);
    const canvas = document.createElement("canvas");
    Object.defineProperty(window, "createEasyRpgPlayer", {
      configurable: true,
      value: vi.fn().mockResolvedValue({
        FS: {}, api: { retromState: () => "{}" }, canvas,
        retromFileSystemReady: false, initApi: vi.fn(), pauseMainLoop: vi.fn(), resumeMainLoop: vi.fn(),
      }),
    });
    const mounting = mountEasyRpg(easyConfig(), target, window, null);
    await vi.waitFor(() => expect(document.head.querySelector("script[data-retrom-rpg-runtime=easyrpg]")).not.toBeNull());
    document.head.querySelector<HTMLScriptElement>("script[data-retrom-rpg-runtime=easyrpg]")
      ?.dispatchEvent(new Event("load"));

    await expect(mounting).rejects.toThrow("RPG_RUNTIME_FILESYSTEM_NOT_READY");
    expect(target.childElementCount).toBe(0);
    expect(document.head.querySelector("script[data-retrom-rpg-runtime=easyrpg]")).toBeNull();
    delete (window as Window & { createEasyRpgPlayer?: unknown }).createEasyRpgPlayer;
    target.remove();
  });

  it("starts the current release after its persistent filesystem is ready", async () => {
    const target = document.createElement("div");
    document.body.append(target);
    const canvas = document.createElement("canvas");
    const createPlayer = vi.fn().mockResolvedValue({
      FS: {},
      api: {
        createRetromCheckpoint: vi.fn(),
        retromState: () => JSON.stringify({
          engine: "RPG2000", ready: true, canCheckpoint: true,
          frameCount: 1, mapId: 1, playerX: 8, playerY: 6, fixtureState: 0,
        }),
      },
      canvas, retromFileSystemReady: true, initApi: vi.fn(), pauseMainLoop: vi.fn(), resumeMainLoop: vi.fn(),
    });
    Object.defineProperty(window, "createEasyRpgPlayer", {
      configurable: true,
      value: createPlayer,
    });
    const config = easyConfig();
    const mounting = mountEasyRpg(config, target, window, null);
    await vi.waitFor(() => expect(document.head.querySelector("script[data-retrom-rpg-runtime=easyrpg]")).not.toBeNull());
    document.head.querySelector<HTMLScriptElement>("script[data-retrom-rpg-runtime=easyrpg]")
      ?.dispatchEvent(new Event("load"));

    const mounted = await mounting;
    expect(createPlayer).toHaveBeenCalledWith(expect.objectContaining({ noExitRuntime: true }));
    expect(mounted.position()).toEqual({ mapId: 1, playerX: 8, playerY: 6, fixtureState: 0 });
    mounted.cleanup();
    delete (window as Window & { createEasyRpgPlayer?: unknown }).createEasyRpgPlayer;
    target.remove();
  });

  it("waits through an incomplete startup state and validates the ready engine", async () => {
    const target = document.createElement("div");
    document.body.append(target);
    const canvas = document.createElement("canvas");
    const retromState = vi.fn()
      .mockReturnValueOnce("{}")
      .mockReturnValue(JSON.stringify({
        engine: "RPG2003", ready: true, canCheckpoint: true,
        frameCount: 1, mapId: 1, playerX: 10, playerY: 8, fixtureState: 0,
      }));
    Object.defineProperty(window, "createEasyRpgPlayer", {
      configurable: true,
      value: vi.fn().mockResolvedValue({
        FS: {}, api: { retromState, createRetromCheckpoint: vi.fn() }, canvas,
        retromFileSystemReady: true, initApi: vi.fn(), pauseMainLoop: vi.fn(), resumeMainLoop: vi.fn(),
      }),
    });
    const mounting = mountEasyRpg(easyConfig("RPG2003"), target, window, null);
    await vi.waitFor(() => expect(document.head.querySelector("script[data-retrom-rpg-runtime=easyrpg]")).not.toBeNull());
    document.head.querySelector<HTMLScriptElement>("script[data-retrom-rpg-runtime=easyrpg]")
      ?.dispatchEvent(new Event("load"));

    const mounted = await mounting;
    expect(retromState).toHaveBeenCalledTimes(2);
    expect(mounted.position()).toEqual({ mapId: 1, playerX: 10, playerY: 8, fixtureState: 0 });
    mounted.cleanup();
    delete (window as Window & { createEasyRpgPlayer?: unknown }).createEasyRpgPlayer;
    target.remove();
  });
});

function easyConfig(generation: "RPG2000" | "RPG2003" = "RPG2000"): EasyConfig {
  const launchId = "01980000-0000-7000-8000-000000000001";
  const root = `/runtime/rpg-project/${launchId}/`;
  const rpg2003 = generation === "RPG2003";
  return {
    runtimeFamily: "RPGMAKER", protocolVersion: 1, mode: "single", purpose: "RPG_RUNTIME_VALIDATION",
    launchId, coreId: "rpgmaker_2000", coreName: "RPG Maker 2000", gameTitle: "Fixture",
    platformName: "RPG Maker", returnTo: "/admin/reviews/item", warnings: [], generation,
    routeKey: rpg2003 ? "RPG2003_EASYRPG_0811_V4" : "RPG2000_EASYRPG_0811_V4",
    artifactId: "01980000-0000-7000-8000-000000000002",
    checkpoint: null, checkpointAvailability: { available: false, reason: "RUNTIME_NOT_READY" },
    runtimeValidation: null,
    adapter: {
      adapterKind: "EASYRPG_WEB", adapterId: "easyrpg-web-v1", engineMode: rpg2003 ? "rpg2k3" : "rpg2k",
      runtimeBaseUrl: "/runtime/rpgmaker/0.8.1.1-v4/", projectRootUrl: root,
      projectIndexUrl: `${root}index.json`, rtpArchive: null, checkpointSlot: 100,
    },
  };
}
