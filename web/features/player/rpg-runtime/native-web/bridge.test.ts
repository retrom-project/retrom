import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { createContext, runInContext, runInNewContext } from "node:vm";
import { describe, expect, it, vi } from "vitest";

type BridgeEvent = {
  data: unknown;
  origin: string;
  ports: FakePort[];
  stopImmediatePropagation: () => void;
};

type FakePort = {
  onmessage: ((event: { data: unknown }) => void) | null;
  postMessage: (message: unknown) => void;
  start: () => void;
};

describe("native-web RPG Maker bridge", () => {
  it("reports the engine ready on the title screen before a map is available", () => {
    const source = readFileSync(
      resolve(process.cwd(), "../data/dat/rpgmaker/v1/native-bridge.js"),
      "utf8",
    );
    const listeners = new Map<string, Array<(event: BridgeEvent) => void>>();
    const replies: unknown[] = [];
    const sceneManager = { _scene: null, updateMain: () => undefined };
    const runtime = {
      DataManager: {},
      SceneManager: sceneManager,
      StorageManager: {},
      Utils: { RPGMAKER_NAME: "MV" },
      addEventListener: (name: string, callback: (event: BridgeEvent) => void) => {
        listeners.set(name, [...(listeners.get(name) ?? []), callback]);
      },
      parent: { postMessage: () => undefined },
      requestAnimationFrame: () => 1,
    };
    runInNewContext(source, { TextDecoder, TextEncoder, window: runtime });
    const port: FakePort = {
      onmessage: null,
      postMessage: (message) => replies.push(message),
      start: () => undefined,
    };

    listeners.get("message")?.[0]?.({
      data: {
        launchId: "01980000-0000-7000-8000-000000000001",
        nonce: "test-nonce",
        parentOrigin: "https://retrom.example",
        profile: "mv-v1",
        protocolVersion: 1,
        type: "RETROM_RPG_NATIVE_CONNECT",
      },
      origin: "https://retrom.example",
      ports: [port],
      stopImmediatePropagation: () => undefined,
    });
    sceneManager.updateMain();

    expect(replies).toContainEqual({
      body: {
        engine: "RPGMV",
        engineProfile: "mv-v1",
        position: { fixtureState: 0, mapId: 0, playerX: 0, playerY: 0 },
      },
      launchId: "01980000-0000-7000-8000-000000000001",
      nonce: "test-nonce",
      protocolVersion: 1,
      requestId: 0,
      type: "READY",
    });
  });

  it("waits for the engine database before restoring an MV save", async () => {
    const source = readFileSync(
      resolve(process.cwd(), "../data/dat/rpgmaker/v1/native-bridge.js"),
      "utf8",
    );
    const listeners = new Map<string, Array<(event: BridgeEvent) => void>>();
    const replies: unknown[] = [];
    const animationFrames: Array<() => void> = [];
    let databaseLoaded = false;
    let loadedSave = "";
    class SceneMap {}
    const storage = {
      exists: (slot: number) => slot < 0,
      load: (slot: number) => String(slot).slice(0, 0),
    };
    const sceneManager = {
      _scene: null as SceneMap | null,
      goto: () => {sceneManager._scene = new SceneMap();},
      updateMain: () => undefined,
    };
    const dataManager = {
      _globalInfo: [] as Array<unknown> | null,
      isDatabaseLoaded: () => databaseLoaded,
      loadGlobalInfo() {
        if (this._globalInfo) {return this._globalInfo;}
        const value = storage.load(0);
        return this._globalInfo = value ? JSON.parse(value) as Array<unknown> : [];
      },
      loadGame(slot: number) {
        if (!this.loadGlobalInfo()[slot]) {return false;}
        loadedSave = storage.load(slot);
        return true;
      },
    };
    const runtime = {
      $gameMap: { isEventRunning: () => false, mapId: () => 1 },
      $gameMessage: { isBusy: () => false },
      $gamePlayer: { x: 11, y: 8 },
      $gameVariables: { value: () => 1 },
      DataManager: dataManager,
      Scene_Map: SceneMap,
      SceneManager: sceneManager,
      StorageManager: storage,
      Utils: { RPGMAKER_NAME: "MV" },
      addEventListener: (name: string, callback: (event: BridgeEvent) => void) => {
        listeners.set(name, [...(listeners.get(name) ?? []), callback]);
      },
      parent: { postMessage: () => undefined },
      requestAnimationFrame: (callback: () => void) => {animationFrames.push(callback); return animationFrames.length;},
    };
    const context = createContext({performance, TextDecoder, TextEncoder, window: runtime});
    runInContext(source, context);
    const saveData = runInContext(
      "Uint8Array.from([115,97,118,101,100,45,97,116,45,98]).buffer",
      context,
    ) as ArrayBuffer;
    const globalInfoJSON = JSON.stringify([...Array(21).fill(null), {title: "fixture"}]);
    const globalInfoValues = [...new TextEncoder().encode(globalInfoJSON)].join(",");
    const globalInfo = runInContext(`Uint8Array.from([${globalInfoValues}]).buffer`, context) as ArrayBuffer;
    const port: FakePort = {
      onmessage: null,
      postMessage: (message) => replies.push(message),
      start: () => undefined,
    };
    const launchId = "01980000-0000-7000-8000-000000000001";
    const nonce = "test-nonce";
    listeners.get("message")?.[0]?.({
      data: { launchId, nonce, parentOrigin: "https://retrom.example", profile: "mv-v1", protocolVersion: 1, type: "RETROM_RPG_NATIVE_CONNECT" },
      origin: "https://retrom.example",
      ports: [port],
      stopImmediatePropagation: () => undefined,
    });

    port.onmessage?.({data: {
      body: {bundle: {
        engine: "RPGMV",
        entries: [
          {data: globalInfo, key: "0", mediaType: "application/octet-stream", store: "LOCAL_STORAGE"},
          {data: saveData, key: "21", mediaType: "application/octet-stream", store: "LOCAL_STORAGE"},
        ],
        resumeSlot: 21,
      }},
      launchId,
      nonce,
      protocolVersion: 1,
      requestId: 1,
      type: "RESTORE",
    }});
    await Promise.resolve();
    expect(loadedSave).toBe("");
    expect(replies.some((reply) => (reply as {type?: string}).type === "RESTORE_RESULT")).toBe(false);

    databaseLoaded = true;
    animationFrames.shift()?.();
    await vi.waitFor(() => expect(replies).toContainEqual({
      body: {position: {fixtureState: 1, mapId: 1, playerX: 11, playerY: 8}},
      launchId,
      nonce,
      protocolVersion: 1,
      requestId: 1,
      type: "RESTORE_RESULT",
    }));
    expect(loadedSave).toBe("saved-at-b");
  });

  it("round-trips MZ saves through JsonEx so restored game objects keep their semantics", async () => {
    const source = readFileSync(
      resolve(process.cwd(), "../data/dat/rpgmaker/v1/native-bridge.js"),
      "utf8",
    );
    const listeners = new Map<string, Array<(event: BridgeEvent) => void>>();
    const replies: unknown[] = [];
    const animationFrames: Array<() => void> = [];
    let restoredMarker = "";
    let mapSceneStarted = true;
    class SceneMap {
      isStarted() {return mapSceneStarted;}
    }
    const storage: {
      exists: (key: string) => boolean;
      loadObject: (key: string) => Promise<unknown>;
      saveObject: (key: string, value: unknown) => Promise<void>;
    } = {
      exists: () => false,
      loadObject: async () => null,
      saveObject: async () => undefined,
    };
    const sceneManager = {
      _scene: new SceneMap(),
      goto: () => {sceneManager._scene = new SceneMap();},
      updateMain: () => undefined,
    };
    const dataManager = {
      isDatabaseLoaded: () => true,
      maxSavefiles: () => 20,
      async saveGame(slot: number) {
        await storage.saveObject(`file${slot}`, { marker: "save-point-b" });
        await storage.saveObject("global", [{ slot }]);
        return true;
      },
      async loadGame(slot: number) {
        const value = await storage.loadObject(`file${slot}`) as { restoredMarker?: () => string } | null;
        restoredMarker = value?.restoredMarker?.() ?? "missing-prototype";
        return true;
      },
    };
    const jsonEx = {
      stringify: (value: unknown) => JSON.stringify({ encodedByJsonEx: true, value }),
      parse: (json: string) => {
        const decoded = JSON.parse(json) as { encodedByJsonEx?: boolean; value?: unknown };
        if (decoded.encodedByJsonEx !== true) {throw new Error("missing JsonEx envelope");}
        return { ...(decoded.value as object), restoredMarker: () => "save-point-b" };
      },
    };
    const runtime = {
      $gameMap: { isEventRunning: () => false, mapId: () => 3 },
      $gameMessage: { isBusy: () => false },
      $gamePlayer: { x: 9, y: 13 },
      $gameVariables: { value: () => 0 },
      DataManager: dataManager,
      ColorManager: {_windowskin: null as {getPixel: () => string} | null},
      Graphics: {width: 0, height: 0},
      JsonEx: jsonEx,
      Scene_Map: SceneMap,
      SceneManager: sceneManager,
      StorageManager: storage,
      Utils: { RPGMAKER_NAME: "MZ" },
      addEventListener: (name: string, callback: (event: BridgeEvent) => void) => {
        listeners.set(name, [...(listeners.get(name) ?? []), callback]);
      },
      parent: { postMessage: () => undefined },
      requestAnimationFrame: (callback: () => void) => {animationFrames.push(callback); return animationFrames.length;},
    };
    const context = createContext({ JSON, performance, TextDecoder, TextEncoder, window: runtime });
    runInContext(source, context);
    const port: FakePort = {
      onmessage: null,
      postMessage: (message) => replies.push(message),
      start: () => undefined,
    };
    const launchId = "01980000-0000-7000-8000-000000000001";
    const nonce = "test-nonce";
    listeners.get("message")?.[0]?.({
      data: { launchId, nonce, parentOrigin: "https://retrom.example", profile: "mz-v1", protocolVersion: 1, type: "RETROM_RPG_NATIVE_CONNECT" },
      origin: "https://retrom.example",
      ports: [port],
      stopImmediatePropagation: () => undefined,
    });

    port.onmessage?.({data: { body: {}, launchId, nonce, protocolVersion: 1, requestId: 1, type: "SAVE" }});
    await vi.waitFor(() => expect(replies.some((reply) => (reply as {type?: string}).type === "SAVE_RESULT")).toBe(true));
    const saveReply = replies.find((reply) => (reply as {type?: string}).type === "SAVE_RESULT") as {
      body: {bundle: {
        engine: string;
        entries: Array<{data: ArrayBuffer; key: string; mediaType: string; store: string}>;
        resumeSlot: number;
      }};
    };
    const fileEntry = saveReply.body.bundle.entries.find((entry) => entry.key === "file21");
    expect(fileEntry && new TextDecoder().decode(fileEntry.data)).toContain('"encodedByJsonEx":true');
    const restoredBundle = {
      ...saveReply.body.bundle,
      entries: saveReply.body.bundle.entries.map((entry) => {
        const values = [...new Uint8Array(entry.data)].join(",");
        return {...entry, data: runInContext(`Uint8Array.from([${values}]).buffer`, context) as ArrayBuffer};
      }),
    };
    port.onmessage?.({data: {
      body: {bundle: restoredBundle},
      launchId,
      nonce,
      protocolVersion: 1,
      requestId: 2,
      type: "RESTORE",
    }});

    await Promise.resolve();
    expect(restoredMarker).toBe("");
    runtime.ColorManager._windowskin = {getPixel: () => "#ffffff"};
    mapSceneStarted = false;
    animationFrames.shift()?.();
    await Promise.resolve();
    expect(restoredMarker).toBe("");
    runtime.Graphics.width = 816;
    runtime.Graphics.height = 624;
    animationFrames.shift()?.();
    await vi.waitFor(() => expect(restoredMarker).toBe("save-point-b"));
    expect(replies).not.toContainEqual(expect.objectContaining({type: "RESTORE_RESULT"}));
    mapSceneStarted = true;
    animationFrames.shift()?.();
    await vi.waitFor(() => expect(replies.length).toBeGreaterThan(1));
    expect(replies).toContainEqual(expect.objectContaining({type: "RESTORE_RESULT"}));
    expect(restoredMarker).toBe("save-point-b");
  });

});

describe("native-web RPG Maker screenshot bridge", () => {
  it("renders a native screenshot from the current scene instead of the discarded WebGL canvas", async () => {
    const source = readFileSync(
      resolve(process.cwd(), "../data/dat/rpgmaker/v1/native-bridge.js"),
      "utf8",
    );
    const listeners = new Map<string, Array<(event: BridgeEvent) => void>>();
    const replies: unknown[] = [];
    const renderedPng = Uint8Array.of(137, 80, 78, 71, 13, 10, 26, 10, 1);
    const destroy = vi.fn();
    let snapAttempt = 0;
    const snap = vi.fn(() => ({
      canvas: {
        toBlob: (callback: BlobCallback, mediaType: string) => {
          snapAttempt += 1;
          callback(snapAttempt <= 3 ? null : new Blob([renderedPng], {type: mediaType}));
        },
      },
      destroy,
    }));
    const animationFrames: FrameRequestCallback[] = [];
    const runtime = {
      Bitmap: {snap},
      SceneManager: {_scene: {marker: "visible-scene"}, updateMain: () => undefined},
      document: {
        querySelector: () => ({
          toBlob: (callback: BlobCallback) => callback(new Blob([Uint8Array.of(0)], {type: "image/png"})),
        }),
      },
      addEventListener: (name: string, callback: (event: BridgeEvent) => void) => {
        listeners.set(name, [...(listeners.get(name) ?? []), callback]);
      },
      parent: {postMessage: () => undefined},
      requestAnimationFrame: (callback: FrameRequestCallback) => {
        animationFrames.push(callback);
        return animationFrames.length;
      },
    };
    runInNewContext(source, {Blob, TextDecoder, TextEncoder, window: runtime});
    const port: FakePort = {
      onmessage: null,
      postMessage: (message) => replies.push(message),
      start: () => undefined,
    };
    const launchId = "01980000-0000-7000-8000-000000000001";
    const nonce = "test-nonce";
    listeners.get("message")?.[0]?.({
      data: {launchId, nonce, parentOrigin: "https://retrom.example", profile: "mv-v1", protocolVersion: 1, type: "RETROM_RPG_NATIVE_CONNECT"},
      origin: "https://retrom.example",
      ports: [port],
      stopImmediatePropagation: () => undefined,
    });

    port.onmessage?.({data: {body: {}, launchId, nonce, protocolVersion: 1, requestId: 1, type: "SCREENSHOT"}});
    expect(snap).not.toHaveBeenCalled();
    animationFrames.shift()?.(0);
    await vi.waitFor(() => expect(animationFrames).toHaveLength(1));
    animationFrames.shift()?.(16);
    await vi.waitFor(() => expect(animationFrames).toHaveLength(1));
    animationFrames.shift()?.(32);
    await vi.waitFor(() => expect(animationFrames).toHaveLength(1));
    animationFrames.shift()?.(48);
    await vi.waitFor(() => expect(replies).toContainEqual(expect.objectContaining({type: "SCREENSHOT_RESULT"})));
    const reply = replies.find((value) => (value as {type?: string}).type === "SCREENSHOT_RESULT") as {
      body: {data: ArrayBuffer; mediaType: string};
    };
    expect([...new Uint8Array(reply.body.data)]).toEqual([...renderedPng]);
    expect(reply.body.mediaType).toBe("image/png");
    expect(snap).toHaveBeenCalledTimes(4);
    expect(snap).toHaveBeenCalledWith(runtime.SceneManager._scene);
    expect(destroy).toHaveBeenCalledTimes(4);
  });
});
