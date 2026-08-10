(() => {
  "use strict";

  const config = window.RETROM_EXAMPLE_CONFIG;
  if (!config || typeof config.core !== "string" || typeof config.gameUrl !== "string") {
    throw new Error("RETROM_EXAMPLE_CONFIG requires core and gameUrl");
  }

  const smoke = {
    core: config.core,
    system: config.system,
    title: config.title,
    phase: "configuring",
    configuredAtMs: Date.now(),
    readyAtMs: null,
    startedAtMs: null,
    framesAdvancingAtMs: null,
    frameStart: null,
    frameNow: null,
    frameDelta: null,
    canvas: null,
    discCount: null,
    discTransitions: [],
    externalFileSizes: [],
    crossOriginIsolated: window.crossOriginIsolated,
    errors: []
  };
  window.__RETROM_SMOKE__ = smoke;
  const runtimeVersion = config.runtimeVersion || "4.2.3";

  const statusText = {
    configuring: `配置 EmulatorJS ${runtimeVersion}`,
    ready: "运行时已就绪",
    started: "核心已启动，等待游戏帧",
    "awaiting-disc-validation": "游戏画面已输出，等待换盘验证",
    "validating-discs": "游戏已启动，正在验证换盘",
    "frames-advancing": "游戏帧持续输出",
    error: "启动失败"
  };

  function renderStatus() {
    const title = document.getElementById("example-title");
    const meta = document.getElementById("example-meta");
    const status = document.getElementById("example-status");
    if (title) title.textContent = config.title;
    if (meta) meta.textContent = `${config.core} · EmulatorJS ${runtimeVersion} · ${config.gameUrl.split("/").pop()}`;
    if (status) {
      status.dataset.phase = smoke.phase;
      status.textContent = statusText[smoke.phase] || smoke.phase;
    }
  }

  function fail(error) {
    const message = error instanceof Error
      ? error.message
      : error?.message || (() => {
        try {
          return JSON.stringify(error);
        } catch {
          return String(error);
        }
      })();
    smoke.errors.push(message);
    if (!smoke.startedAtMs) smoke.phase = "error";
    renderStatus();
  }

  function installExternalFileCompatibility() {
    if (runtimeVersion !== "4.2.3" || Object.keys(config.externalFiles || {}).length === 0) return;
    let constructor = window.EJS_GameManager;
    const patch = value => {
      const prototype = value?.prototype;
      const original = prototype?.writeFile;
      if (!prototype || typeof original !== "function") throw new Error("External file compatibility is unavailable");
      if (original.retromNormalizesArrayBuffer === true) return;
      const normalizedWriteFile = function(path, data) {
        const bytes = Object.prototype.toString.call(data) === "[object ArrayBuffer]"
          ? new Uint8Array(data)
          : data;
        return original.call(this, path, bytes);
      };
      Object.defineProperty(normalizedWriteFile, "retromNormalizesArrayBuffer", { value: true });
      prototype.writeFile = normalizedWriteFile;
    };
    if (constructor) patch(constructor);
    Object.defineProperty(window, "EJS_GameManager", {
      configurable: true,
      enumerable: true,
      get: () => constructor,
      set: value => {
        patch(value);
        constructor = value;
      }
    });
  }

  window.addEventListener("error", event => fail(event.error || event.message));
  window.addEventListener("unhandledrejection", event => fail(event.reason));
  document.addEventListener("DOMContentLoaded", renderStatus);

  function resizeCanvasToCss() {
    const canvas = document.querySelector("canvas.ejs_canvas") || window.EJS_emulator?.canvas;
    const rect = canvas?.getBoundingClientRect();
    const host = document.getElementById("game");
    if (!canvas) return;
    const width = rect?.width || host?.clientWidth || 1;
    const height = rect?.height || host?.clientHeight || 1;
    canvas.width = Math.max(1, Math.round(width * devicePixelRatio));
    canvas.height = Math.max(1, Math.round(height * devicePixelRatio));
  }

  async function validateDiscSequence() {
    const manager = window.EJS_emulator?.gameManager;
    const expectedCount = config.expectedDiscCount;
    if (!manager || !Number.isInteger(expectedCount)) throw new Error("Multi-disc game manager is unavailable");
    const count = manager.getDiskCount?.();
    let current = manager.getCurrentDisk?.();
    if (count !== expectedCount || !Number.isInteger(current) || current < 0 || current >= count) {
      throw new Error(`Unexpected disc state count=${count} current=${current}`);
    }
    smoke.discCount = count;
    const sequence = [...Array(count).keys()].filter(index => index !== current);
    if (current !== 0) sequence.push(0);
    else if (sequence.at(-1) !== 0) sequence.push(0);
    for (const target of sequence) {
      const frameBefore = manager.getFrameNum();
      manager.setCurrentDisk?.(target);
      await new Promise(resolve => window.setTimeout(resolve, 1000));
      const observed = manager.getCurrentDisk?.();
      const frameAfter = manager.getFrameNum();
      smoke.discTransitions.push({ from: current, target, observed, frameBefore, frameAfter });
      if (observed !== target) throw new Error(`Disc switch did not reach index ${target}: observed ${observed}`);
      if (!(frameAfter > frameBefore)) throw new Error(`Frames did not advance after switching to disc ${target}`);
      current = observed;
    }
  }

  installExternalFileCompatibility();
  let discValidationStarted = false;
  window.__RETROM_VALIDATE_DISCS__ = async () => {
    if (!config.expectedDiscCount || smoke.phase !== "awaiting-disc-validation" || discValidationStarted) {
      throw new Error("Multi-disc validation is not ready");
    }
    discValidationStarted = true;
    smoke.phase = "validating-discs";
    renderStatus();
    try {
      await validateDiscSequence();
      smoke.phase = "frames-advancing";
      smoke.framesAdvancingAtMs = Date.now();
      renderStatus();
    } catch (error) {
      smoke.phase = "error";
      fail(error);
      renderStatus();
      throw error;
    }
  };

  window.EJS_player = "#game";
  window.EJS_DEBUG_XX = config.debug === true;
  window.EJS_core = config.core;
  window.EJS_gameUrl = config.gameUrl;
  window.EJS_gameName = `retrom-smoke-${config.core}`;
  window.EJS_gameID = config.gameId;
  window.EJS_pathtodata = `/data/runtime/emulatorjs/${runtimeVersion}/data/`;
  window.EJS_startOnLoaded = config.dosArchiveMode !== true;
  window.EJS_dontExtractRom = config.dosArchiveMode === true;
  window.EJS_disableBatchBootup = config.dosArchiveMode === true;
  window.EJS_fullscreenOnLoaded = false;
  window.EJS_threads = config.threads === true;
  window.EJS_volume = 0;
  window.EJS_language = "en-US";
  window.EJS_disableAutoLang = false;
  window.EJS_disableDatabases = true;
  window.EJS_disableLocalStorage = true;
  window.EJS_CacheLimit = 0;
  window.EJS_backgroundColor = "#05060a";
  window.EJS_color = "#7558e8";
  window.EJS_paths = config.coreArtifactUrl && config.coreArtifactFilename
    ? { [config.coreArtifactFilename]: config.coreArtifactUrl }
    : {};
  window.EJS_externalFiles = { ...(config.externalFiles || {}) };
  // Chrome is the only supported browser in phase one. Pin WebGL2 so the smoke
  // suite exercises the modern 4.2.3 core artifact instead of the legacy build.
  window.EJS_defaultOptions = {
    webgl2Enabled: "enabled",
    ...(config.defaultOptions || {})
  };

  if (config.biosUrl) window.EJS_biosUrl = config.biosUrl;
  if (config.parentUrl) window.EJS_gameParentUrl = config.parentUrl;

  window.EJS_ready = () => {
    if (config.expectedDiscCount) {
      const instance = window.EJS_emulator;
      if (!instance) throw new Error("Multi-disc emulator instance is unavailable");
      if (instance.allSettings === undefined) instance.allSettings = {};
      if (Object.prototype.toString.call(instance.allSettings) !== "[object Object]") {
        throw new Error("Multi-disc settings initialization failed");
      }
    }
    if (config.dosArchiveMode === true) {
      const dontExtractIfCore = window.EJS_emulator?.downloadType?.rom?.dontExtractIfCore;
      if (!Array.isArray(dontExtractIfCore)) throw new Error("DOS archive mode is unavailable");
      if (!dontExtractIfCore.includes(config.core)) dontExtractIfCore.push(config.core);
    }
    smoke.phase = "ready";
    smoke.readyAtMs = Date.now();
    if (config.workarounds?.resizeCanvasToCss) resizeCanvasToCss();
    renderStatus();
    if (config.dosArchiveMode === true) {
      const clickStart = () => {
        const startButton = document.querySelector(".ejs_start_button");
        if (!startButton) return false;
        startButton.click();
        return true;
      };
      if (!clickStart()) {
        const observer = new MutationObserver(() => {
          if (clickStart()) observer.disconnect();
        });
        observer.observe(document.documentElement, { childList: true, subtree: true });
      }
    }
  };

  window.EJS_onGameStart = () => {
    smoke.phase = "started";
    smoke.startedAtMs = Date.now();
    try {
      smoke.frameStart = window.EJS_emulator.gameManager.getFrameNum();
      if (config.expectedDiscCount) {
        const fs = window.EJS_emulator.gameManager.FS;
        smoke.externalFileSizes = Object.keys(config.externalFiles || {}).sort().map(path => fs.stat(path).size);
        if (smoke.externalFileSizes.length !== config.expectedDiscCount || smoke.externalFileSizes.some(size => size <= 0)) {
          throw new Error(`Unexpected external file sizes: ${smoke.externalFileSizes.join(",")}`);
        }
      }
    } catch (error) {
      smoke.phase = "error";
      fail(new Error(`Unable to validate the initial runtime: ${error.message}`));
      return;
    }
    renderStatus();

    if (config.workarounds?.resizeCanvasToCss) {
      window.setTimeout(resizeCanvasToCss, 100);
    }

    for (const input of config.startupInputs || []) {
      window.setTimeout(() => {
        try {
          window.EJS_emulator.gameManager.simulateInput(
            input.player || 0,
            input.control,
            1
          );
          window.setTimeout(
            () => window.EJS_emulator.gameManager.simulateInput(
              input.player || 0,
              input.control,
              0
            ),
            input.durationMs || 120
          );
        } catch (error) {
          smoke.errors.push(`Startup input: ${error.message}`);
        }
      }, input.delayMs);
    }

    const timer = window.setInterval(() => {
      try {
        const canvas = document.querySelector("canvas.ejs_canvas");
        const rect = canvas?.getBoundingClientRect();
        smoke.frameNow = window.EJS_emulator.gameManager.getFrameNum();
        smoke.frameDelta = Number.isFinite(smoke.frameStart) ? smoke.frameNow - smoke.frameStart : null;
        smoke.canvas = canvas ? {
          width: canvas.width,
          height: canvas.height,
          cssWidth: Math.round(rect.width),
          cssHeight: Math.round(rect.height)
        } : null;

        const canvasReady = smoke.canvas
          && smoke.canvas.width >= 100
          && smoke.canvas.height >= 100
          && smoke.canvas.cssWidth >= 100
          && smoke.canvas.cssHeight >= 100;
        if (canvasReady && smoke.frameDelta >= 120) {
          window.clearInterval(timer);
          if (config.expectedDiscCount) {
            smoke.phase = "awaiting-disc-validation";
            renderStatus();
          } else {
            smoke.phase = "frames-advancing";
            smoke.framesAdvancingAtMs = Date.now();
            renderStatus();
          }
        }
      } catch (error) {
        smoke.errors.push(`Frame monitor: ${error.message}`);
      }
    }, 250);
  };
})();
