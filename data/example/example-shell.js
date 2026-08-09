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
    crossOriginIsolated: window.crossOriginIsolated,
    errors: []
  };
  window.__RETROM_SMOKE__ = smoke;
  const runtimeVersion = config.runtimeVersion || "4.2.3";

  const statusText = {
    configuring: `配置 EmulatorJS ${runtimeVersion}`,
    ready: "运行时已就绪",
    started: "核心已启动，等待游戏帧",
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
    } catch (error) {
      smoke.errors.push(`Unable to read initial frame counter: ${error.message}`);
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
          smoke.phase = "frames-advancing";
          smoke.framesAdvancingAtMs = Date.now();
          window.clearInterval(timer);
          renderStatus();
        }
      } catch (error) {
        smoke.errors.push(`Frame monitor: ${error.message}`);
      }
    }, 250);
  };
})();
