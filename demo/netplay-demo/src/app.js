import { EmulatorBridge } from "./emulator-bridge.js";
import { getGameConfig } from "./catalog.js";
import { NetplaySession } from "./netplay-session.js";
import { LocalRelay } from "./local-relay.js";
import { WebSocketRelay } from "./websocket-relay.js";

const elements = {
  core: document.querySelector("[data-core]"),
  transport: document.querySelector("[data-transport]"),
  inputDelay: document.querySelector("[data-input-delay]"),
  joinDelay: document.querySelector("[data-join-delay]"),
  latency: document.querySelector("[data-latency]"),
  target: document.querySelector("[data-target]"),
  start: document.querySelector("[data-start]"),
  pause: document.querySelector("[data-pause]"),
  hash: document.querySelector("[data-hash]"),
  state: document.querySelector("[data-session-state]"),
  stateLabel: document.querySelector("[data-state-label]"),
  initialHash: document.querySelector("[data-initial-hash]"),
  lateJoin: document.querySelector("[data-late-join]"),
  checkpoints: document.querySelector("[data-checkpoints]"),
  transportLabel: document.querySelector("[data-transport-label]"),
  frameDelta: document.querySelector("[data-frame-delta]"),
  log: document.querySelector("[data-log]"),
  frames: [...document.querySelectorAll("iframe[data-player]")]
};

let session = null;
let authorityBridge = null;
let launchController = null;
let mountedBridges = [];
let paused = false;
let preJoinInputEvents = 0;
let logEntries = [];
let latestReport = { state: "idle", clients: [] };
const preJoinInputTimers = new Set();

function wait(delayMs, signal) {
  if (delayMs === 0) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", abort);
      resolve();
    }, delayMs);
    const abort = () => {
      clearTimeout(timer);
      reject(new DOMException("Late join cancelled", "AbortError"));
    };
    signal?.addEventListener("abort", abort, { once: true });
  });
}

function selectQueryValue(element, value) {
  if (value !== null && [...element.options].some((option) => option.value === value)) {
    element.value = value;
  }
}

function readHashInterval() {
  const value = Number.parseInt(new URLSearchParams(window.location.search).get("hashEvery") ?? "120", 10);
  return Number.isSafeInteger(value) && value >= 1 && value <= 3000 ? value : 120;
}

function setGlobalStatus(report = latestReport) {
  latestReport = report;
  window.__NETPLAY_DEMO_STATUS__ = structuredClone({
    phase: report.state ?? "idle",
    core: elements.core.value,
    transport: elements.transport.value,
    inputDelay: Number.parseInt(elements.inputDelay.value, 10),
    latencyMs: Number.parseInt(elements.latency.value, 10),
    targetFrame: Number.parseInt(elements.target.value, 10),
    report
  });
}

function appendLog(message) {
  logEntries = [message, ...logEntries].slice(0, 9);
  elements.log.replaceChildren(...logEntries.map((entry) => {
    const item = document.createElement("li");
    item.textContent = entry;
    return item;
  }));
}

function setSessionState(state, report) {
  const labels = {
    idle: "Idle",
    loading: "Loading cores",
    hosting: "Host playing solo",
    joining: "Peer joining",
    synchronizing: "Seeding state",
    running: "Rollback sync",
    resynchronizing: "Resynchronizing",
    reconnecting: "Reconnecting",
    paused: "Paused",
    complete: "Proof passed",
    failed: "Desync / error",
    closed: "Closed"
  };
  elements.state.dataset.sessionState = state;
  elements.stateLabel.textContent = labels[state] ?? state;
  const activeStates = ["loading", "hosting", "joining", "synchronizing", "running", "paused", "resynchronizing", "reconnecting"];
  elements.start.disabled = activeStates.includes(state);
  elements.pause.disabled = !["running", "paused"].includes(state);
  elements.hash.disabled = state !== "running";
  elements.pause.textContent = state === "paused" ? "Resume" : "Pause";
  for (const control of [elements.core, elements.transport, elements.inputDelay, elements.joinDelay, elements.target]) {
    control.disabled = activeStates.includes(state);
  }
  if (state === "complete") appendLog(`PASS · ${report.targetFrame} frames · ${report.desyncs} desync`);
  if (state === "failed") appendLog(`FAIL · ${report.error ?? "unknown error"}`);
  if (report?.lateJoin) {
    elements.lateJoin.textContent = report.lateJoin.peerMounted
      ? `${report.lateJoin.authoritySoloFrames ?? 0} solo frames · epoch ${report.lateJoin.epoch ?? "pending"}`
      : `A solo · B in ${report.lateJoin.configuredDelayMs / 1000} s`;
  }
  setGlobalStatus(report);
}

function renderReport(report) {
  latestReport = report;
  const clients = report.clients ?? [];
  clients.forEach((metrics, slot) => {
    for (const name of ["emulatorFrame", "netFrame", "predictionDepth", "rollbackCount"]) {
      const output = document.querySelector(`[data-metric="${slot}:${name}"]`);
      if (output) output.textContent = String(metrics[name] ?? "—");
    }
    const hashOutput = document.querySelector(`[data-metric="${slot}:lastHash"]`);
    if (hashOutput) {
      hashOutput.textContent = metrics.lastHash
        ? `${metrics.lastHash.digest.slice(0, 16)}… @ ${metrics.lastHash.frame}`
        : "—";
    }
    const badge = document.querySelector(`[data-sync="${slot}"]`);
    if (badge) {
      const stalled = metrics.blockers?.includes("prediction-window");
      const failed = metrics.failed || report.state === "failed";
      const replaying = metrics.replay?.active;
      badge.dataset.phase = failed ? "failed" : stalled ? "stalled" : "sync";
      badge.textContent = failed ? "Failed" : replaying ? "Replaying" : stalled ? `Bound @ ${metrics.waitingFrame}` : "Predicting";
    }
  });
  if (clients.length === 2) {
    elements.frameDelta.textContent = `Δ frame ${Math.abs(clients[0].netFrame - clients[1].netFrame)}`;
  }
  elements.initialHash.textContent = report.initialStateDigest
    ? `${report.initialStateDigest.slice(0, 12)}… / ${report.initialStateBytes} B`
    : "—";
  elements.checkpoints.textContent = `${report.hashCheckpoints?.length ?? 0} / ${report.resyncs ?? 0} resync`;
  elements.transportLabel.textContent = report.transport === "websocket"
    ? "WebSocket authoritative relay"
    : "In-page canonical relay";
  if (report.profile) {
    const profileLabel = `EJS ${report.profile.emulatorjsVersion} · ${report.profile.core} · ${report.profile.gameSha256.slice(0, 8)}…`;
    for (const output of document.querySelectorAll("[data-profile]")) output.textContent = profileLabel;
  }
  setGlobalStatus(report);
}

function frameLoaded(frame) {
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error("Player iframe load timed out")), 10000);
    frame.addEventListener("load", () => {
      clearTimeout(timeout);
      resolve();
    }, { once: true });
  });
}

async function mountPlayer(core, slot) {
  const nonce = Date.now();
  const frame = elements.frames[slot];
  const side = slot === 0 ? "left" : "right";
  const loaded = frameLoaded(frame);
  frame.src = `./player.html?core=${encodeURIComponent(core)}&side=${side}&v=${nonce}`;
  document.querySelector(`[data-empty="${slot}"]`).hidden = true;
  await loaded;
  return EmulatorBridge.connect(frame, side);
}

function unmountPlayer(slot) {
  elements.frames[slot].src = "about:blank";
  document.querySelector(`[data-empty="${slot}"]`).hidden = false;
}

function simulateInput(slot, control, value) {
  if (session) session.simulateLocalInput(slot, control, value);
  else if (slot === 0 && authorityBridge) {
    authorityBridge.simulateLocalInput(control, value);
    preJoinInputEvents += 1;
  }
}

function pressInput(slot, control, durationMs = 120) {
  if (session) {
    session.press(slot, control, durationMs);
    return;
  }
  if (slot !== 0 || !authorityBridge) return;
  simulateInput(slot, control, 1);
  const timer = setTimeout(() => {
    preJoinInputTimers.delete(timer);
    simulateInput(slot, control, 0);
  }, durationMs);
  preJoinInputTimers.add(timer);
}

async function start() {
  launchController?.abort();
  launchController = new AbortController();
  session?.cleanup();
  session = null;
  mountedBridges.forEach((bridge) => bridge?.pause());
  mountedBridges = [];
  authorityBridge = null;
  for (const timer of preJoinInputTimers) clearTimeout(timer);
  preJoinInputTimers.clear();
  preJoinInputEvents = 0;
  elements.frames.forEach((_, slot) => unmountPlayer(slot));
  paused = false;
  logEntries = [];
  appendLog("Loading two isolated EmulatorJS 4.2.3 runtimes…");
  setSessionState("loading", { state: "loading", clients: [] });
  try {
    getGameConfig(elements.core.value);
    const joinDelayMs = Number.parseInt(elements.joinDelay.value, 10);
    const authority = await mountPlayer(elements.core.value, 0);
    mountedBridges[0] = authority;
    authorityBridge = authority;
    const hostStartedAtMs = Date.now();
    const authorityFrameAtHostStart = authority.getFrame();
    const hostingReport = {
      state: "hosting",
      clients: [],
      lateJoin: {
        enabled: joinDelayMs > 0,
        configuredDelayMs: joinDelayMs,
        hostStartedAtMs,
        peerMounted: false,
        authorityFrameAtHostStart
      }
    };
    appendLog(`Client A is playing solo; Client B joins in ${(joinDelayMs / 1000).toFixed(0)} s.`);
    setSessionState("hosting", hostingReport);
    await wait(joinDelayMs, launchController.signal);

    authority.pause();
    const authorityFrameAtJoin = await authority.waitForPause();
    const peerMountStartedAtMs = Date.now();
    const lateJoin = {
      ...hostingReport.lateJoin,
      actualSoloDurationMs: peerMountStartedAtMs - hostStartedAtMs,
      authorityFrameAtJoin,
      authoritySoloFrames: authorityFrameAtJoin - authorityFrameAtHostStart,
      authoritySoloInputEvents: preJoinInputEvents,
      peerMountStartedAtMs,
      peerMounted: true
    };
    appendLog(`Client B is joining at host frame ${authorityFrameAtJoin}.`);
    setSessionState("joining", { state: "joining", clients: [], lateJoin });
    const peer = await mountPlayer(elements.core.value, 1);
    mountedBridges[1] = peer;
    lateJoin.peerReadyAtMs = Date.now();
    const bridges = [authority, peer];
    appendLog("Both real cores are ready; loading the host's live state into Client B.");
    const transport = elements.transport.value;
    const relay = transport === "websocket"
      ? new WebSocketRelay({ latencyMs: Number.parseInt(elements.latency.value, 10) })
      : new LocalRelay({ latencyMs: Number.parseInt(elements.latency.value, 10) });
    session = new NetplaySession({
      bridges,
      relay,
      transport,
      inputDelay: Number.parseInt(elements.inputDelay.value, 10),
      latencyMs: Number.parseInt(elements.latency.value, 10),
      hashEvery: readHashInterval(),
      lateJoin,
      targetFrame: Number.parseInt(elements.target.value, 10),
      onStatus: ({ report }) => renderReport(report),
      onState: ({ state, report }) => {
        setSessionState(state, report);
        if (state === "synchronizing") appendLog("Aligning deterministic initial state at a frame boundary…");
        if (state === "running") appendLog("Bounded prediction and rollback are active.");
        if (state === "resynchronizing") appendLog("Hash mismatch detected; transferring authority savestate…");
        if (state === "reconnecting") appendLog("Transport lease held; waiting for WebSocket resume…");
      }
    });
    await session.start();
    launchController = null;
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") return;
    const message = error instanceof Error ? error.message : String(error);
    appendLog(`Startup failed: ${message}`);
    const report = session?.getReport() ?? { state: "failed", error: message, clients: [] };
    setSessionState("failed", report);
  }
}

elements.start.addEventListener("click", () => void start());
elements.pause.addEventListener("click", () => {
  if (!session) return;
  paused = !paused;
  if (paused) session.pause();
  else session.resume();
});
elements.hash.addEventListener("click", () => {
  if (!session) return;
  appendLog(`Manual state checkpoint scheduled for frame ${session.hashNow()}.`);
});
elements.latency.addEventListener("change", () => {
  if (!session) return;
  const latency = Number.parseInt(elements.latency.value, 10);
  session.setLatency(latency);
  appendLog(`Injected RTT changed to ${latency} ms.`);
});

for (const button of document.querySelectorAll("[data-input]")) {
  const [slot, control] = button.dataset.input.split(":").map(Number);
  const release = () => simulateInput(slot, control, 0);
  button.addEventListener("pointerdown", (event) => {
    event.preventDefault();
    button.setPointerCapture(event.pointerId);
    simulateInput(slot, control, 1);
  });
  button.addEventListener("pointerup", release);
  button.addEventListener("pointercancel", release);
  button.addEventListener("keydown", (event) => {
    if ((event.key === " " || event.key === "Enter") && !event.repeat) simulateInput(slot, control, 1);
  });
  button.addEventListener("keyup", (event) => {
    if (event.key === " " || event.key === "Enter") release();
  });
}

window.__NETPLAY_DEMO__ = {
  start,
  getStatus: () => {
    const status = structuredClone(window.__NETPLAY_DEMO_STATUS__);
    if (session) {
      status.report = session.getReport();
      status.phase = status.report.state;
    } else if (authorityBridge && status.phase === "hosting") {
      status.report.lateJoin.authorityCurrentFrame = authorityBridge.getFrame();
      status.report.lateJoin.authoritySoloInputEvents = preJoinInputEvents;
    }
    return status;
  },
  press: pressInput,
  hashNow: () => session?.hashNow(),
  injectDesync: (slot, control, durationMs) => session?.injectDesync(slot, control, durationMs),
  dropConnection: (slot) => session?.dropConnection(slot),
  cleanup
};
setGlobalStatus();

const params = new URLSearchParams(window.location.search);
selectQueryValue(elements.core, params.get("core"));
selectQueryValue(elements.transport, params.get("transport"));
selectQueryValue(elements.joinDelay, params.get("joinAfter"));
selectQueryValue(elements.target, params.get("target"));
selectQueryValue(elements.latency, params.get("latency"));
if (params.get("autostart") === "1") void start();

function cleanup() {
  launchController?.abort();
  launchController = null;
  for (const timer of preJoinInputTimers) clearTimeout(timer);
  preJoinInputTimers.clear();
  session?.cleanup();
  session = null;
  mountedBridges.forEach((bridge) => bridge?.pause());
  mountedBridges = [];
  authorityBridge = null;
  elements.frames.forEach((_, slot) => unmountPlayer(slot));
}

window.addEventListener("pagehide", cleanup, { once: true });
