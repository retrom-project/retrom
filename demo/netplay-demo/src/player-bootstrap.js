import { getGameConfig, RUNTIME_VERSION } from "./catalog.js";
import { installEmulatorJs423NetplayPatch } from "../patches/ejs-4.2.3-netplay.js";

const params = new URLSearchParams(window.location.search);
const side = params.get("side") === "right" ? "right" : "left";
const config = getGameConfig(params.get("core") ?? "nes");
const status = {
  side,
  phase: "configuring",
  profile: {
    emulatorjsVersion: RUNTIME_VERSION,
    adapterId: "ejs-4.2.3-netplay-poc-v1",
    core: config.core,
    system: config.system,
    gameSha256: config.gameSha256,
    biosSha256: config.biosSha256 ?? null,
    coreSha256: config.coreSha256,
    threads: false,
    persistentStorage: false
  },
  capabilities: null,
  errors: []
};
window.__NETPLAY_PLAYER__ = status;

function render() {
  document.documentElement.dataset.phase = status.phase;
  const label = document.querySelector("[data-player-status]");
  if (label) label.textContent = status.phase === "started" ? `${config.label} ready` : status.phase;
}

function fail(error) {
  status.phase = "error";
  status.errors.push(error instanceof Error ? error.message : String(error));
  render();
}

function detectCapabilities() {
  const emulator = window.EJS_emulator;
  const manager = emulator?.gameManager;
  const module = emulator?.Module;
  const patch = window.__RETROM_EJS_423_PATCH__;
  return {
    version: emulator?.ejs_version,
    inputCapture: typeof manager?.simulateInput === "function",
    rawInputInjection: typeof manager?.functions?.simulateInput === "function",
    frameCounter: typeof manager?.getFrameNum === "function",
    frameHook: Boolean(module) && patch?.frameHookBootstrap === true,
    pauseResume: typeof manager?.toggleMainLoop === "function",
    stateCapture: typeof manager?.getState === "function",
    stateLoad: typeof manager?.loadState === "function",
    patch
  };
}

window.addEventListener("error", (event) => fail(event.error ?? event.message));
window.addEventListener("unhandledrejection", (event) => fail(event.reason));
document.addEventListener("DOMContentLoaded", render);

installEmulatorJs423NetplayPatch(window);
window.EJS_player = "#game";
window.EJS_DEBUG_XX = false;
window.EJS_EXPERIMENTAL_NETPLAY = false;
window.EJS_core = config.core;
window.EJS_gameUrl = config.gameUrl;
window.EJS_gameName = `retrom-netplay-${config.id}-${side}`;
window.EJS_gameID = config.gameId;
window.EJS_pathtodata = "./vendor/emulatorjs-4.2.3/data/";
window.EJS_startOnLoaded = true;
window.EJS_fullscreenOnLoaded = false;
window.EJS_threads = false;
window.EJS_volume = 0;
window.EJS_language = "en-US";
window.EJS_disableAutoLang = false;
window.EJS_disableDatabases = true;
window.EJS_disableLocalStorage = true;
window.EJS_CacheLimit = 0;
window.EJS_backgroundColor = "#05070b";
window.EJS_color = side === "left" ? "#72e6b1" : "#ffb769";
window.EJS_defaultOptions = { webgl2Enabled: "enabled", rewindEnabled: "disabled" };
if (config.biosUrl) window.EJS_biosUrl = config.biosUrl;
window.EJS_Buttons = {
  netplay: { visible: false },
  saveState: { visible: false },
  loadState: { visible: false },
  saveSavFiles: { visible: false },
  loadSavFiles: { visible: false },
  cheat: { visible: false },
  restart: { visible: false }
};

window.EJS_ready = () => {
  status.phase = "runtime-ready";
  render();
};

window.EJS_onGameStart = () => {
  try {
    status.capabilities = detectCapabilities();
    const missing = Object.entries(status.capabilities)
      .filter(([name, value]) => name !== "patch" && name !== "version" && value !== true)
      .map(([name]) => name);
    if (status.capabilities.version !== RUNTIME_VERSION || missing.length > 0) {
      throw new Error(`Missing EmulatorJS netplay capabilities: ${missing.join(", ")}`);
    }
    if (!status.capabilities.patch?.inMemorySaves) {
      throw new Error("Per-iframe in-memory saves patch was not installed");
    }
    status.phase = "started";
    render();
  } catch (error) {
    fail(error);
  }
};

const loader = document.createElement("script");
loader.src = "./vendor/emulatorjs-4.2.3/data/loader.js";
loader.addEventListener("error", () => fail(new Error("Unable to load EmulatorJS 4.2.3")));
document.head.appendChild(loader);
