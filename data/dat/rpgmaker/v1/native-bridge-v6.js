(function installRetromRpgNativeBridge(global) {
  "use strict";

  const PROTOCOL_VERSION = 1;
  const MAX_CONTROL_BYTES = 64 * 1024;
  const MAX_ENTRY_BYTES = 64 * 1024 * 1024;
  const MAX_SCREENSHOT_BYTES = 10 * 1024 * 1024;
  const encoder = new TextEncoder();
  const decoder = new TextDecoder("utf-8", { fatal: true });
  let launchId = null;
  let nonce = null;
  let parentOrigin = null;
  let port = null;
  let profile = null;
  let lastRequestId = 0;
  let requestPending = false;
  let frameCount = 0;
  let engineReadySent = false;
  let inputObserved = false;
  let audioObserved = false;
  let sceneManagerHooked = false;

  function ownKeys(value, expected) {
    if (!value || typeof value !== "object" || Array.isArray(value)) return false;
    const keys = Object.keys(value).sort();
    return keys.length === expected.length && keys.every((key, index) => key === expected[index]);
  }

  function engine() {
    if (profile === "mv-v1") return "RPGMV";
    if (profile === "mz-v1") return "RPGMZ";
    throw new Error("RPG_NATIVE_PROFILE_INVALID");
  }

  function envelope(requestId, type, body) {
    return { protocolVersion: PROTOCOL_VERSION, launchId, nonce, requestId, type, body };
  }

  function send(requestId, type, body, transfer) {
    if (!port) return;
    const message = envelope(requestId, type, body);
    if (encoder.encode(JSON.stringify(message)).byteLength > MAX_CONTROL_BYTES) {
      throw new Error("RPG_NATIVE_CONTROL_TOO_LARGE");
    }
    port.postMessage(message, transfer || []);
  }

  function event(type, body) {
    send(0, type, body);
  }

  function position() {
    const map = global.$gameMap;
    const player = global.$gamePlayer;
    const variables = global.$gameVariables;
    const result = {
      mapId: map && typeof map.mapId === "function" ? map.mapId() : 0,
      playerX: player && Number.isInteger(player.x) ? player.x : 0,
      playerY: player && Number.isInteger(player.y) ? player.y : 0,
      fixtureState: variables && typeof variables.value === "function" ? variables.value(1) : 0,
    };
    if (!Number.isSafeInteger(result.mapId) || result.mapId < 0 ||
      !Number.isSafeInteger(result.playerX) || !Number.isSafeInteger(result.playerY) ||
      !Number.isSafeInteger(result.fixtureState)) {
      throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");
    }
    return result;
  }

  function readyForCheckpoint() {
    const scene = global.SceneManager && global.SceneManager._scene;
    const sceneMap = global.Scene_Map;
    const messageBusy = global.$gameMessage && typeof global.$gameMessage.isBusy === "function" &&
      global.$gameMessage.isBusy();
    const eventBusy = global.$gameMap && typeof global.$gameMap.isEventRunning === "function" &&
      global.$gameMap.isEventRunning();
    return Boolean(scene && sceneMap && scene instanceof sceneMap && !messageBusy && !eventBusy &&
      global.DataManager && global.StorageManager && position().mapId > 0);
  }

  function engineRuntimeReady() {
    const manager = global.SceneManager;
    const utils = global.Utils;
    const expectedName = profile === "mv-v1" ? "MV" : "MZ";
    return Boolean(manager && typeof manager.updateMain === "function" && global.DataManager &&
      global.StorageManager && utils && utils.RPGMAKER_NAME === expectedName);
  }

  function storageNames(slot) {
    return profile === "mz-v1" ? ["global", `file${slot}`] : ["0", String(slot)];
  }

  function replaceMethod(target, name, create, restorers) {
    if (!target || typeof target[name] !== "function") return false;
    const original = target[name];
    target[name] = create(original);
    restorers.push(function restoreMethod() { target[name] = original; });
    return true;
  }

  async function captureNativeSave() {
    if (!readyForCheckpoint()) throw new Error("RPG_CHECKPOINT_UNAVAILABLE");
    const slot = Number(global.DataManager.maxSavefiles()) + 1;
    if (!Number.isSafeInteger(slot) || slot < 1) throw new Error("RPG_CHECKPOINT_SLOT_INVALID");
    const captured = new Map();
    const storage = global.StorageManager;
    const restorers = [];
    const allowed = new Set(storageNames(slot));
    let supported = false;
    if (profile === "mv-v1") {
      supported = replaceMethod(storage, "save", (original) => function retromSave(savefileId, json) {
        const key = String(savefileId);
        if (!allowed.has(key)) return original.apply(this, arguments);
        const bytes = encoder.encode(String(json));
        if (!bytes.byteLength || bytes.byteLength > MAX_ENTRY_BYTES) throw new Error("RPG_CHECKPOINT_TOO_LARGE");
        captured.set(key, bytes);
        return undefined;
      }, restorers);
    } else {
      supported = replaceMethod(storage, "saveObject", (original) => async function retromSaveObject(saveName, object) {
        const key = String(saveName);
        if (!allowed.has(key)) return original.apply(this, arguments);
        const jsonEx = global.JsonEx;
        if (!jsonEx || typeof jsonEx.stringify !== "function") {
          throw new Error("RPG_CHECKPOINT_STORAGE_UNSUPPORTED");
        }
        const serialized = jsonEx.stringify(object);
        if (typeof serialized !== "string") throw new Error("RPG_CHECKPOINT_CREATE_FAILED");
        const bytes = encoder.encode(serialized);
        if (!bytes.byteLength || bytes.byteLength > MAX_ENTRY_BYTES) throw new Error("RPG_CHECKPOINT_TOO_LARGE");
        captured.set(key, bytes);
        return undefined;
      }, restorers);
    }
    if (!supported) throw new Error("RPG_CHECKPOINT_STORAGE_UNSUPPORTED");
    try {
      const saved = await global.DataManager.saveGame(slot);
      if (saved === false || !captured.has(storageNames(slot)[1])) throw new Error("RPG_CHECKPOINT_CREATE_FAILED");
    } finally {
      restorers.reverse().forEach((restore) => restore());
    }
    const entries = [...captured.entries()].map(([key, bytes]) => ({
      store: profile === "mz-v1" ? "LOCALFORAGE" : "LOCAL_STORAGE",
      key,
      mediaType: "application/octet-stream",
      data: bytes.slice().buffer,
    }));
    return { bundle: { engine: engine(), resumeSlot: slot, entries }, position: position() };
  }

  function validateRestoreBundle(bundle) {
    if (!bundle || bundle.engine !== engine() || !Number.isSafeInteger(bundle.resumeSlot) ||
      bundle.resumeSlot < 1 || !Array.isArray(bundle.entries) || bundle.entries.length < 1 || bundle.entries.length > 2) {
      throw new Error("RPG_CHECKPOINT_RESTORE_INVALID");
    }
    const allowed = new Set(storageNames(bundle.resumeSlot));
    const expectedStore = profile === "mz-v1" ? "LOCALFORAGE" : "LOCAL_STORAGE";
    const entries = new Map();
    for (const entry of bundle.entries) {
      if (!ownKeys(entry, ["data", "key", "mediaType", "store"]) || entry.store !== expectedStore ||
        typeof entry.key !== "string" || !allowed.has(entry.key) || entries.has(entry.key) ||
        !(entry.data instanceof ArrayBuffer) || !entry.data.byteLength || entry.data.byteLength > MAX_ENTRY_BYTES) {
        throw new Error("RPG_CHECKPOINT_RESTORE_INVALID");
      }
      entries.set(entry.key, new Uint8Array(entry.data));
    }
    if (!entries.has(storageNames(bundle.resumeSlot)[1])) throw new Error("RPG_CHECKPOINT_RESTORE_INVALID");
    return { slot: bundle.resumeSlot, entries };
  }

  async function restoreNativeSave(bundle) {
    const validated = validateRestoreBundle(bundle);
    await waitForDatabase();
    await waitForMZRestoreRuntime();
    const storage = global.StorageManager;
    const restorers = [];
    let supported = false;
    if (profile === "mv-v1") {
      supported = replaceMethod(storage, "load", (original) => function retromLoad(savefileId) {
        const value = validated.entries.get(String(savefileId));
        return value ? decoder.decode(value) : original.apply(this, arguments);
      }, restorers);
      replaceMethod(storage, "exists", (original) => function retromExists(savefileId) {
        return validated.entries.has(String(savefileId)) || original.apply(this, arguments);
      }, restorers);
    } else {
      supported = replaceMethod(storage, "loadObject", (original) => async function retromLoadObject(saveName) {
        const value = validated.entries.get(String(saveName));
        if (!value) return original.apply(this, arguments);
        const jsonEx = global.JsonEx;
        if (!jsonEx || typeof jsonEx.parse !== "function") {
          throw new Error("RPG_CHECKPOINT_STORAGE_UNSUPPORTED");
        }
        return jsonEx.parse(decoder.decode(value));
      }, restorers);
      replaceMethod(storage, "exists", (original) => function retromExists(saveName) {
        return validated.entries.has(String(saveName)) || original.apply(this, arguments);
      }, restorers);
    }
    if (!supported) throw new Error("RPG_CHECKPOINT_STORAGE_UNSUPPORTED");
    if (profile === "mv-v1") global.DataManager._globalInfo = null;
    try {
      const loaded = await global.DataManager.loadGame(validated.slot);
      if (loaded === false) throw new Error("RPG_CHECKPOINT_RESTORE_FAILED");
      if (global.SceneManager && global.Scene_Map) global.SceneManager.goto(global.Scene_Map);
      await waitForMap();
    } finally {
      restorers.reverse().forEach((restore) => restore());
    }
    return { position: position() };
  }

  function waitForDatabase() {
    const manager = global.DataManager;
    if (!manager || typeof manager.isDatabaseLoaded !== "function") {
      return Promise.reject(new Error("RPG_CHECKPOINT_DATABASE_UNAVAILABLE"));
    }
    const deadline = performance.now() + 30000;
    return new Promise((resolve, reject) => {
      function poll() {
        if (manager.isDatabaseLoaded()) { resolve(); return; }
        if (performance.now() >= deadline) { reject(new Error("RPG_CHECKPOINT_DATABASE_TIMEOUT")); return; }
        global.requestAnimationFrame(poll);
      }
      poll();
    });
  }

  function waitForMZRestoreRuntime() {
    if (profile !== "mz-v1") return Promise.resolve();
    const deadline = performance.now() + 30000;
    return new Promise((resolve, reject) => {
      function poll() {
        const manager = global.SceneManager;
        const colors = global.ColorManager;
        const windowskin = colors && colors._windowskin;
        if (manager && manager._scene && windowskin && typeof windowskin.getPixel === "function") {
          resolve();
          return;
        }
        if (performance.now() >= deadline) { reject(new Error("RPG_CHECKPOINT_RESTORE_TIMEOUT")); return; }
        global.requestAnimationFrame(poll);
      }
      poll();
    });
  }

  function waitForMap() {
    const deadline = performance.now() + 30000;
    return new Promise((resolve, reject) => {
      function poll() {
        const scene = global.SceneManager && global.SceneManager._scene;
        const sceneStarted = scene && (typeof scene.isStarted !== "function" || scene.isStarted());
        if (sceneStarted && readyForCheckpoint()) { resolve(); return; }
        if (performance.now() >= deadline) { reject(new Error("RPG_CHECKPOINT_RESTORE_TIMEOUT")); return; }
        global.requestAnimationFrame(poll);
      }
      poll();
    });
  }

  async function screenshot() {
    for (let attempt = 0; attempt < 60; attempt += 1) {
      await new Promise((resolve) => global.requestAnimationFrame(resolve));
      try {
        const capture = screenshotCanvas();
        if (capture) return await encodeScreenshot(capture);
      } catch {
        // A newly restored WebGL scene may not have presented an extractable frame yet.
      }
    }
    throw new Error("PLAYER_SCREENSHOT_UNAVAILABLE");
  }

  function encodeScreenshot(capture) {
    return new Promise((resolve, reject) => {
      try {
        capture.canvas.toBlob((blob) => {
          if (!blob || !blob.size || blob.size > MAX_SCREENSHOT_BYTES) {
            capture.release();
            reject(new Error("PLAYER_SCREENSHOT_UNAVAILABLE"));
            return;
          }
          blob.arrayBuffer().then((data) => {
            capture.release();
            resolve({ data, mediaType: blob.type || "image/png" });
          }, (error) => {
            capture.release();
            reject(error);
          });
        }, "image/png");
      } catch (error) {
        capture.release();
        reject(error);
      }
    });
  }

  function screenshotCanvas() {
    const bitmapType = global.Bitmap;
    const scene = global.SceneManager && global.SceneManager._scene;
    if (bitmapType && typeof bitmapType.snap === "function" && scene) {
      const bitmap = bitmapType.snap(scene);
      const canvas = bitmap && bitmap.canvas;
      if (!canvas || typeof canvas.toBlob !== "function") {
        if (bitmap && typeof bitmap.destroy === "function") bitmap.destroy();
        throw new Error("PLAYER_SCREENSHOT_UNAVAILABLE");
      }
      return {
        canvas,
        release: function releaseBitmap() {
          if (typeof bitmap.destroy === "function") bitmap.destroy();
        },
      };
    }
    const canvas = global.document.querySelector("canvas");
    if (!canvas || typeof canvas.toBlob !== "function") return null;
    return { canvas, release: function releaseCanvas() {} };
  }

  function setPaused(paused) {
    const manager = global.SceneManager;
    if (!manager) throw new Error("RPG_RUNTIME_CONTROL_UNAVAILABLE");
    if (paused && typeof manager.stop === "function") manager.stop();
    if (!paused && typeof manager.resume === "function") manager.resume();
    else if (!paused && typeof manager.requestUpdate === "function") {
      manager._stopped = false;
      manager.requestUpdate();
    }
  }

  function setVolume(value) {
    if (typeof value !== "number" || !Number.isFinite(value) || value < 0 || value > 1 ||
      !global.WebAudio || typeof global.WebAudio.setMasterVolume !== "function") {
      throw new Error("RPG_RUNTIME_CONTROL_UNAVAILABLE");
    }
    global.WebAudio.setMasterVolume(value);
  }

  async function dispatch(message) {
    switch (message.type) {
    case "PROBE": return { type: "PROBE_RESULT", body: { ready: readyForCheckpoint(), frameCount, position: position() } };
    case "SAVE": return { type: "SAVE_RESULT", body: await captureNativeSave() };
    case "RESTORE": return { type: "RESTORE_RESULT", body: await restoreNativeSave(message.body.bundle) };
    case "SCREENSHOT": return { type: "SCREENSHOT_RESULT", body: await screenshot() };
    case "PAUSE": setPaused(true); return { type: "PAUSE_RESULT", body: {} };
    case "RESUME": setPaused(false); return { type: "RESUME_RESULT", body: {} };
    case "SET_VOLUME": setVolume(message.body.value); return { type: "SET_VOLUME_RESULT", body: {} };
    case "CLEANUP":
      await global.fetch("/__retrom/cleanup", { method: "POST", credentials: "same-origin", keepalive: true });
      return { type: "CLEANUP_RESULT", body: {} };
    default: throw new Error("RPG_NATIVE_MESSAGE_INVALID");
    }
  }

  async function receive(message) {
    if (!ownKeys(message, ["body", "launchId", "nonce", "protocolVersion", "requestId", "type"]) ||
      message.protocolVersion !== PROTOCOL_VERSION || message.launchId !== launchId || message.nonce !== nonce ||
      !Number.isSafeInteger(message.requestId) || message.requestId !== lastRequestId + 1 ||
      typeof message.type !== "string" || !ownKeys(message.body, Object.keys(message.body || {}).sort()) || requestPending ||
      encoder.encode(JSON.stringify(message)).byteLength > MAX_CONTROL_BYTES) {
      return;
    }
    lastRequestId = message.requestId;
    requestPending = true;
    try {
      const result = await dispatch(message);
      const transfers = [];
      if (result.type === "SAVE_RESULT") result.body.bundle.entries.forEach((entry) => transfers.push(entry.data));
      if (result.type === "SCREENSHOT_RESULT") transfers.push(result.body.data);
      send(message.requestId, result.type, result.body, transfers);
    } catch (error) {
      send(message.requestId, "ERROR", { code: error && error.message || "RPG_NATIVE_RUNTIME_FAILED" });
    } finally {
      requestPending = false;
    }
  }

  function observeFrame() {
    frameCount += 1;
    if (!engineReadySent && engineRuntimeReady()) {
      engineReadySent = true;
      event("READY", { engine: engine(), engineProfile: profile, position: position() });
    }
    if (frameCount === 300) event("FRAMES", { continuousFrames: frameCount });
  }

  function installSceneManagerHook() {
    const manager = global.SceneManager;
    if (sceneManagerHooked || !manager || typeof manager.updateMain !== "function") return;
    const original = manager.updateMain;
    manager.updateMain = function retromUpdateMain() {
      const result = original.apply(this, arguments);
      observeFrame();
      return result;
    };
    sceneManagerHooked = true;
  }

  function pollForEngine() {
    installSceneManagerHook();
    if (!sceneManagerHooked) global.requestAnimationFrame(pollForEngine);
  }

  function installAudioObserver() {
    const prototype = global.AudioBufferSourceNode && global.AudioBufferSourceNode.prototype;
    if (!prototype || typeof prototype.start !== "function") return;
    const original = prototype.start;
    prototype.start = function retromAudioStart() {
      if (!audioObserved) { audioObserved = true; event("AUDIO", { observed: true }); }
      return original.apply(this, arguments);
    };
  }

  global.addEventListener("keydown", () => {
    if (!inputObserved) { inputObserved = true; event("INPUT", { observed: true }); }
  }, true);

  global.addEventListener("message", function connect(eventMessage) {
    const message = eventMessage.data;
    if (!ownKeys(message, ["launchId", "nonce", "parentOrigin", "profile", "protocolVersion", "type"]) ||
      message.type !== "RETROM_RPG_NATIVE_CONNECT" || message.protocolVersion !== PROTOCOL_VERSION ||
      typeof message.launchId !== "string" || typeof message.nonce !== "string" ||
      typeof message.parentOrigin !== "string" || eventMessage.origin !== message.parentOrigin ||
      (message.profile !== "mv-v1" && message.profile !== "mz-v1") || eventMessage.ports.length !== 1 || port) {
      return;
    }
    eventMessage.stopImmediatePropagation();
    launchId = message.launchId;
    nonce = message.nonce;
    parentOrigin = message.parentOrigin;
    profile = message.profile;
    port = eventMessage.ports[0];
    port.onmessage = (portEvent) => { void receive(portEvent.data); };
    port.start();
    pollForEngine();
    installAudioObserver();
  }, true);

  global.parent.postMessage({ type: "RETROM_RPG_NATIVE_BRIDGE_READY", protocolVersion: PROTOCOL_VERSION }, "*");
  global.__RETROM_RPG_NATIVE_BRIDGE__ = Object.freeze({ protocolVersion: PROTOCOL_VERSION });
})(window);
