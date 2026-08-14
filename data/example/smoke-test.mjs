#!/usr/bin/env node

import { spawn } from "node:child_process";
import { inflateSync } from "node:zlib";
import { promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(SCRIPT_DIR, "../..");
const MANIFEST_PATH = path.join(SCRIPT_DIR, "fixtures.json");
export function resolveResultsDirectory(environment = process.env) {
  const configured = environment.RETROM_EXAMPLE_RESULTS_DIR?.trim();
  return configured ? path.resolve(REPO_ROOT, configured) : path.join(SCRIPT_DIR, "results");
}

export function resolveResultPath(filename, environment = process.env) {
  return path.join(resolveResultsDirectory(environment), filename);
}

const RESULTS_DIR = resolveResultsDirectory();
const PORT = Number.parseInt(process.env.RETROM_EXAMPLE_PORT || "4173", 10);
const BASE_URL = `http://127.0.0.1:${PORT}`;

const sleep = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));

async function stopProcess(child) {
  if (!child || child.exitCode !== null) return;
  const exited = new Promise(resolve => child.once("exit", resolve));
  child.kill("SIGTERM");
  await Promise.race([exited, sleep(3000)]);
  if (child.exitCode === null) {
    child.kill("SIGKILL");
    await Promise.race([exited, sleep(3000)]);
  }
}

async function materializeDOSFixtures() {
  const child = spawn(
    "/usr/bin/python3",
    [path.join(SCRIPT_DIR, "materialize-dos-fixture.py")],
    { cwd: REPO_ROOT, stdio: ["ignore", "inherit", "inherit"] }
  );
  const code = await new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", resolve);
  });
  if (code !== 0) throw new Error(`DOS fixture materialization failed with code ${code}`);
}

class CdpPipe {
  constructor(process) {
    this.process = process;
    this.nextId = 1;
    this.buffer = Buffer.alloc(0);
    this.pending = new Map();
    this.listeners = new Map();
    process.stdio[4].on("data", chunk => this.consume(chunk));
    process.stdio[4].on("error", error => this.rejectAll(error));
    process.on("exit", (code, signal) => {
      this.rejectAll(new Error(`Chrome exited early (code=${code}, signal=${signal})`));
    });
  }

  consume(chunk) {
    this.buffer = Buffer.concat([this.buffer, chunk]);
    let separator;
    while ((separator = this.buffer.indexOf(0)) !== -1) {
      const payload = this.buffer.subarray(0, separator).toString("utf8");
      this.buffer = this.buffer.subarray(separator + 1);
      if (!payload) continue;
      const message = JSON.parse(payload);
      if (message.id !== undefined) {
        const pending = this.pending.get(message.id);
        if (!pending) continue;
        this.pending.delete(message.id);
        clearTimeout(pending.timer);
        if (message.error) {
          pending.reject(new Error(`${message.error.message} (${message.error.code})`));
        } else {
          pending.resolve(message.result || {});
        }
        continue;
      }
      for (const listener of this.listeners.get(message.method) || []) {
        listener(message.params || {}, message.sessionId);
      }
    }
  }

  rejectAll(error) {
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(error);
    }
    this.pending.clear();
  }

  on(method, listener) {
    const listeners = this.listeners.get(method) || [];
    listeners.push(listener);
    this.listeners.set(method, listeners);
  }

  call(method, params = {}, sessionId = undefined, timeoutMs = 30000) {
    const id = this.nextId++;
    const message = { id, method, params };
    if (sessionId) message.sessionId = sessionId;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`CDP timeout: ${method}`));
      }, timeoutMs);
      this.pending.set(id, { resolve, reject, timer });
      this.process.stdio[3].write(`${JSON.stringify(message)}\0`, error => {
        if (!error) return;
        clearTimeout(timer);
        this.pending.delete(id);
        reject(error);
      });
    });
  }
}

async function evaluate(cdp, sessionId, expression, options = {}) {
  const result = await cdp.call(
    "Runtime.evaluate",
    {
      expression,
      returnByValue: true,
      awaitPromise: true,
      userGesture: options.userGesture === true
    },
    sessionId
  );
  if (result.exceptionDetails) {
    const detail = result.exceptionDetails.exception?.description
      || result.exceptionDetails.text
      || "Runtime.evaluate failed";
    throw new Error(detail);
  }
  return result.result?.value;
}

async function waitForServer(server) {
  const deadline = Date.now() + 20000;
  let lastError;
  while (Date.now() < deadline) {
    if (server.exitCode !== null) {
      throw new Error(`Example server exited with code ${server.exitCode}`);
    }
    try {
      const response = await fetch(`${BASE_URL}/__health`, { cache: "no-store" });
      if (response.ok) return;
    } catch (error) {
      lastError = error;
    }
    await sleep(200);
  }
  throw new Error(`Example server did not become ready: ${lastError?.message || "timeout"}`);
}

async function executable(pathname) {
  try {
    await fs.access(pathname, 0o1);
    return true;
  } catch {
    return false;
  }
}

const SYSTEM_CHROME_BINARIES = [
  "/usr/bin/google-chrome",
  "/usr/bin/chromium",
  "/usr/bin/chromium-browser"
];

export async function resolveChromeBinary(
  environment = process.env,
  homeDirectory = os.homedir(),
  systemCandidates = SYSTEM_CHROME_BINARIES
) {
  if (environment.RETROM_CHROME_BIN) {
    if (await executable(environment.RETROM_CHROME_BIN)) return environment.RETROM_CHROME_BIN;
    throw new Error(`RETROM_CHROME_BIN is not executable: ${environment.RETROM_CHROME_BIN}`);
  }
  for (const candidate of systemCandidates) {
    if (await executable(candidate)) return candidate;
  }
  const browserRoot = environment.PLAYWRIGHT_BROWSERS_PATH || path.join(homeDirectory, ".cache", "ms-playwright");
  let entries = [];
  try {
    entries = await fs.readdir(browserRoot, { withFileTypes: true });
  } catch {
    // A missing Playwright cache is reported by the stable error below.
  }
  const versions = entries.filter(entry => entry.isDirectory() && /^chromium-\d+$/.test(entry.name)).map(entry => entry.name).sort((left, right) => right.localeCompare(left, "en", { numeric: true }));
  for (const version of versions) {
    for (const relative of ["chrome-linux64/chrome", "chrome-linux/chrome"]) {
      const candidate = path.join(browserRoot, version, relative);
      if (await executable(candidate)) return candidate;
    }
  }
  throw new Error("No supported Chrome binary found; install Playwright Chromium or set RETROM_CHROME_BIN");
}

function launchChrome(chromeBinary, profileDirectory) {
  const args = [
    "--remote-debugging-pipe",
    "--no-sandbox",
    "--disable-dev-shm-usage",
    "--no-first-run",
    "--no-default-browser-check",
    "--autoplay-policy=no-user-gesture-required",
    "--enable-unsafe-swiftshader",
    "--use-angle=swiftshader",
    "--hide-scrollbars",
    "--window-size=1280,800",
    `--user-data-dir=${profileDirectory}`,
    "about:blank"
  ];
  if (process.env.RETROM_CHROME_HEADFUL !== "1") args.unshift("--headless=new");
  return spawn(chromeBinary, args, {
    cwd: REPO_ROOT,
    stdio: ["ignore", "ignore", "pipe", "pipe", "pipe"]
  });
}

function paeth(left, above, upperLeft) {
  const estimate = left + above - upperLeft;
  const leftDistance = Math.abs(estimate - left);
  const aboveDistance = Math.abs(estimate - above);
  const diagonalDistance = Math.abs(estimate - upperLeft);
  if (leftDistance <= aboveDistance && leftDistance <= diagonalDistance) return left;
  if (aboveDistance <= diagonalDistance) return above;
  return upperLeft;
}

function inspectPng(buffer) {
  const signature = "89504e470d0a1a0a";
  if (buffer.subarray(0, 8).toString("hex") !== signature) {
    throw new Error("Screenshot is not a PNG file");
  }

  let offset = 8;
  let width;
  let height;
  let bitDepth;
  let colorType;
  const compressed = [];
  while (offset < buffer.length) {
    const length = buffer.readUInt32BE(offset);
    const type = buffer.subarray(offset + 4, offset + 8).toString("ascii");
    const data = buffer.subarray(offset + 8, offset + 8 + length);
    offset += length + 12;
    if (type === "IHDR") {
      width = data.readUInt32BE(0);
      height = data.readUInt32BE(4);
      bitDepth = data[8];
      colorType = data[9];
    } else if (type === "IDAT") {
      compressed.push(data);
    } else if (type === "IEND") {
      break;
    }
  }

  const channelCount = { 0: 1, 2: 3, 4: 2, 6: 4 }[colorType];
  if (!width || !height || bitDepth !== 8 || !channelCount) {
    throw new Error(`Unsupported screenshot PNG: ${width}x${height}, depth=${bitDepth}, type=${colorType}`);
  }

  const encoded = inflateSync(Buffer.concat(compressed));
  const stride = width * channelCount;
  const pixels = Buffer.alloc(stride * height);
  let encodedOffset = 0;
  for (let y = 0; y < height; y += 1) {
    const filter = encoded[encodedOffset++];
    const rowStart = y * stride;
    const previousStart = (y - 1) * stride;
    for (let x = 0; x < stride; x += 1) {
      const raw = encoded[encodedOffset++];
      const left = x >= channelCount ? pixels[rowStart + x - channelCount] : 0;
      const above = y > 0 ? pixels[previousStart + x] : 0;
      const upperLeft = y > 0 && x >= channelCount
        ? pixels[previousStart + x - channelCount]
        : 0;
      let value;
      if (filter === 0) value = raw;
      else if (filter === 1) value = raw + left;
      else if (filter === 2) value = raw + above;
      else if (filter === 3) value = raw + Math.floor((left + above) / 2);
      else if (filter === 4) value = raw + paeth(left, above, upperLeft);
      else throw new Error(`Unsupported PNG filter ${filter}`);
      pixels[rowStart + x] = value & 0xff;
    }
  }

  const sampleStride = Math.max(1, Math.floor(Math.sqrt((width * height) / 100000)));
  const colors = new Set();
  let samples = 0;
  let nonBlack = 0;
  let sum = 0;
  let sumSquares = 0;
  for (let y = 0; y < height; y += sampleStride) {
    for (let x = 0; x < width; x += sampleStride) {
      const pixelOffset = (y * width + x) * channelCount;
      let red;
      let green;
      let blue;
      if (colorType === 0 || colorType === 4) {
        red = green = blue = pixels[pixelOffset];
      } else {
        red = pixels[pixelOffset];
        green = pixels[pixelOffset + 1];
        blue = pixels[pixelOffset + 2];
      }
      const luminance = 0.2126 * red + 0.7152 * green + 0.0722 * blue;
      colors.add(`${red >> 4}:${green >> 4}:${blue >> 4}`);
      samples += 1;
      if (luminance > 12) nonBlack += 1;
      sum += luminance;
      sumSquares += luminance * luminance;
    }
  }
  const mean = sum / samples;
  const variance = Math.max(0, sumSquares / samples - mean * mean);
  return {
    width,
    height,
    sampledPixels: samples,
    colorBuckets: colors.size,
    nonBlackRatio: Number((nonBlack / samples).toFixed(4)),
    meanLuminance: Number(mean.toFixed(2)),
    luminanceStdDev: Number(Math.sqrt(variance).toFixed(2))
  };
}

function visualScore(stats) {
  return stats.colorBuckets + stats.luminanceStdDev * 3 + stats.nonBlackRatio * 20;
}

function visualLooksRendered(stats) {
  return stats.colorBuckets >= 3
    && stats.luminanceStdDev >= 5
    && stats.nonBlackRatio >= 0.01;
}

async function canvasClip(cdp, sessionId) {
  return evaluate(cdp, sessionId, `(() => {
    const canvases = [...document.querySelectorAll("canvas")]
      .map(canvas => ({ canvas, rect: canvas.getBoundingClientRect() }))
      .filter(item => item.rect.width >= 2 && item.rect.height >= 2)
      .sort((a, b) => b.rect.width * b.rect.height - a.rect.width * a.rect.height);
    if (!canvases.length) return null;
    const rect = canvases[0].rect;
    const x = Math.max(0, rect.left);
    const y = Math.max(0, rect.top);
    const width = Math.min(innerWidth - x, rect.right - x);
    const height = Math.min(innerHeight - y, rect.bottom - y);
    if (width < 2 || height < 2) return null;
    return { x, y, width, height, scale: 1 };
  })()`);
}

async function takeScreenshot(cdp, sessionId, outputPath, attempts = 1) {
  let best;
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    const clip = await canvasClip(cdp, sessionId);
    const params = {
      format: "png",
      fromSurface: true,
      captureBeyondViewport: false
    };
    if (clip) params.clip = clip;
    const capture = await cdp.call("Page.captureScreenshot", params, sessionId, 30000);
    const buffer = Buffer.from(capture.data, "base64");
    const stats = inspectPng(buffer);
    if (!best || visualScore(stats) > visualScore(best.stats)) {
      best = { buffer, stats, canvasCropUsed: Boolean(clip) };
    }
    if (attempt + 1 < attempts) await sleep(1000);
  }
  await fs.writeFile(outputPath, best.buffer);
  return { ...best.stats, canvasCropUsed: best.canvasCropUsed };
}

async function waitForPage(cdp, sessionId, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let lastState = null;
  while (Date.now() < deadline) {
    try {
      lastState = await evaluate(cdp, sessionId, `(() => ({
        readyState: document.readyState,
        smoke: window.__RETROM_SMOKE__ ? JSON.parse(JSON.stringify(window.__RETROM_SMOKE__)) : null,
        bodyText: (document.body?.innerText || "").slice(0, 1000),
        crossOriginIsolated: window.crossOriginIsolated
      }))()`);
      if (lastState?.smoke?.phase === "error") {
        throw new Error(`Example reported an error: ${lastState.smoke.errors.join("; ")}`);
      }
      if (lastState?.bodyText?.includes("Failed to start game")) {
        throw new Error("EmulatorJS displayed 'Failed to start game'");
      }
      if (["awaiting-disc-validation", "frames-advancing"].includes(lastState?.smoke?.phase)) return lastState;
    } catch (error) {
      if (
        String(error.message).startsWith("Example reported an error:")
        || String(error.message).startsWith("EmulatorJS displayed")
      ) throw error;
    }
    await sleep(500);
  }
  throw new Error(
    `Timed out waiting for frames; last state: ${JSON.stringify(lastState?.smoke || lastState)}`
  );
}

async function runFixture(cdp, fixture) {
  const startedAtMs = Date.now();
  const runId = fixture.runId || fixture.core;
  const screenshotPath = path.join(RESULTS_DIR, `${runId}.png`);
  const screenshotRelative = path.relative(REPO_ROOT, screenshotPath).split(path.sep).join("/");
  const initialScreenshotPath = path.join(RESULTS_DIR, `${runId}-initial.png`);
  const initialScreenshotRelative = path.relative(REPO_ROOT, initialScreenshotPath).split(path.sep).join("/");
  const pageUrl = `${BASE_URL}/${fixture.examplePath}${fixture.exampleQuery || ""}`;
  const coreRequests = [];
  const externalFileRequests = [];
  const expectedExternalFiles = [
    ...(fixture.expectedExternalFiles || []),
    ...(fixture.bios || []).map(record => record.localPath),
    ...(fixture.runtimeFiles || []).map(record => record.path)
  ];
  const consoleErrors = [];
  const consoleMessages = [];
  let browserContextId;
  let sessionId;
  let state;
  let visual;
  let initialVisual;
  let postSwitchVisual;
  let failure;

  try {
    ({ browserContextId } = await cdp.call("Target.createBrowserContext"));
    const { targetId } = await cdp.call("Target.createTarget", {
      url: "about:blank",
      browserContextId,
      width: 1280,
      height: 800
    });
    ({ sessionId } = await cdp.call("Target.attachToTarget", { targetId, flatten: true }));
    await Promise.all([
      cdp.call("Page.enable", {}, sessionId),
      cdp.call("Runtime.enable", {}, sessionId),
      cdp.call("Network.enable", {}, sessionId)
    ]);

    cdp.on("Network.responseReceived", (params, eventSessionId) => {
      if (eventSessionId !== sessionId) return;
      const url = params.response?.url || "";
      if (url.includes("/cores/") || url.includes("/overrides/")) {
        coreRequests.push({ url, status: params.response.status });
      }
      if (expectedExternalFiles.some(
        expected => url === new URL(`/${expected}`, BASE_URL).href
      )) {
        externalFileRequests.push({ url, status: params.response.status });
      }
    });
    cdp.on("Network.loadingFailed", (params, eventSessionId) => {
      if (eventSessionId === sessionId && params.errorText) {
        consoleErrors.push(`Network: ${params.errorText} (${params.type || "unknown"})`);
      }
    });
    cdp.on("Runtime.exceptionThrown", (params, eventSessionId) => {
      if (eventSessionId !== sessionId) return;
      const exception = params.exceptionDetails;
      consoleErrors.push(
        exception?.exception?.description || exception?.text || "Uncaught page exception"
      );
    });
    cdp.on("Runtime.consoleAPICalled", (params, eventSessionId) => {
      if (eventSessionId !== sessionId) return;
      const text = params.args.map(argument => argument.value ?? argument.description ?? "").join(" ");
      consoleMessages.push(`${params.type}: ${text}`);
      if (["error", "warning"].includes(params.type)) {
        consoleErrors.push(`${params.type}: ${text}`);
      }
    });

    await cdp.call("Page.navigate", { url: pageUrl }, sessionId, 30000);
    const interactionDeadline = Date.now() + 15000;
    while (Date.now() < interactionDeadline) {
      try {
        const readyState = await evaluate(cdp, sessionId, "document.readyState");
        if (readyState === "interactive" || readyState === "complete") break;
      } catch {
        // The execution context is briefly unavailable during navigation.
      }
      await sleep(100);
    }
    await evaluate(
      cdp,
      sessionId,
      `(() => {
        document.body?.click();
        document.querySelector("button.ejs_start_button, .ejs_start_button")?.click();
        return true;
      })()`,
      { userGesture: true }
    );

    state = await waitForPage(cdp, sessionId, fixture.timeoutMs);
    if ((fixture.core === "dosbox_pure" || fixture.requiresThreads) && !state.crossOriginIsolated) {
      throw new Error(`${fixture.core} requires crossOriginIsolated=true for its threaded core`);
    }
    const expectedCoreArtifactUrl = `${BASE_URL}/${fixture.coreArtifact.path}`;
    const loadedExpectedCore = coreRequests.some(
      request => request.url === expectedCoreArtifactUrl
        && request.status >= 200
        && request.status < 300
    );
    if (!loadedExpectedCore) {
      throw new Error(`Expected core artifact was not loaded: ${expectedCoreArtifactUrl}`);
    }
    for (const expected of expectedExternalFiles) {
      const expectedURL = new URL(`/${expected}`, BASE_URL).href;
      if (!externalFileRequests.some(request => request.url === expectedURL && request.status >= 200 && request.status < 300)) {
        throw new Error(`Expected external file was not loaded: ${expectedURL}`);
      }
    }
    if (Array.isArray(fixture.discs)) {
      const expectedDiscSizes = [...fixture.discs]
        .sort((left, right) => left.index - right.index)
        .map(disc => disc.size);
      const mountedDiscSizes = state.smoke.externalFileSizes;
      if (JSON.stringify(mountedDiscSizes) !== JSON.stringify(expectedDiscSizes)) {
        throw new Error(`Mounted external file sizes did not match the fixture: expected=${expectedDiscSizes.join(",")} observed=${(mountedDiscSizes || []).join(",")}`);
      }
      await sleep(fixture.settleMs);
      initialVisual = await takeScreenshot(cdp, sessionId, initialScreenshotPath, 4);
      if (!visualLooksRendered(initialVisual)) {
        throw new Error(`Canvas did not contain a non-uniform game image before disc switching: ${JSON.stringify(initialVisual)}`);
      }
      await evaluate(cdp, sessionId, "window.__RETROM_VALIDATE_DISCS__()", { userGesture: true });
      state = await waitForPage(cdp, sessionId, fixture.timeoutMs);
    }
    await sleep(fixture.settleMs);
    state = await evaluate(cdp, sessionId, `(() => ({
      smoke: JSON.parse(JSON.stringify(window.__RETROM_SMOKE__)),
      crossOriginIsolated: window.crossOriginIsolated
    }))()`);
    visual = await takeScreenshot(cdp, sessionId, screenshotPath, 4);
    if (Array.isArray(fixture.discs)) postSwitchVisual = visual;
    if (!Array.isArray(fixture.discs) && !visualLooksRendered(visual)) {
      throw new Error(`Canvas did not contain a non-uniform game image: ${JSON.stringify(visual)}`);
    }
  } catch (error) {
    failure = error instanceof Error ? error.message : String(error);
    if (sessionId && !visual) {
      try {
        visual = await takeScreenshot(cdp, sessionId, screenshotPath, 1);
      } catch (screenshotError) {
        consoleErrors.push(`Screenshot: ${screenshotError.message}`);
      }
    }
  } finally {
    if (browserContextId) {
      try {
        await cdp.call("Target.disposeBrowserContext", { browserContextId });
      } catch {
        // Browser cleanup errors do not change the test result.
      }
    }
  }

  const finishedAtMs = Date.now();
  return {
    core: runId,
    productCore: fixture.core,
    formatId: fixture.formatId || null,
    playerAdapterId: fixture.playerAdapterId || null,
    status: failure ? "failed" : "passed",
    failure: failure || null,
    startedAtMs,
    finishedAtMs,
    durationMs: finishedAtMs - startedAtMs,
    pageUrl,
    initialScreenshotPath: initialVisual ? initialScreenshotRelative : null,
    screenshotPath: initialVisual ? initialScreenshotRelative : visual ? screenshotRelative : null,
    postSwitchScreenshotPath: postSwitchVisual ? screenshotRelative : null,
    smoke: state?.smoke || null,
    crossOriginIsolated: state?.crossOriginIsolated ?? null,
    visual: initialVisual || visual || null,
    initialVisual: initialVisual || null,
    postSwitchVisual: postSwitchVisual || null,
    expectedCoreArtifact: fixture.coreArtifact,
    requestedCoreAssets: [...new Map(coreRequests.map(item => [item.url, item])).values()],
    requestedExternalFiles: [...new Map(externalFileRequests.map(item => [item.url, item])).values()],
    consoleErrors: [...new Set(consoleErrors)].slice(0, 30),
    consoleMessages: consoleMessages.slice(-100)
  };
}

export function expandFixtureRuns(allFixtures, requestedCores = []) {
  const fixtures = requestedCores.length
    ? allFixtures.filter(fixture => requestedCores.includes(fixture.selector || fixture.core))
    : allFixtures;
  const unknown = requestedCores.filter(
    core => !allFixtures.some(fixture => (fixture.selector || fixture.core) === core)
  );
  if (unknown.length) throw new Error(`Unknown core(s): ${unknown.join(", ")}`);
  return fixtures.flatMap(fixture => {
    if (fixture.core !== "ppsspp") return [fixture];
    const iso = fixture.formatVariants?.find(variant => variant.formatId === "iso");
    if (!iso || fixture.game?.formatId !== "cso") {
      throw new Error("PPSSPP requires fixed cso and iso format variants");
    }
    return [
      { ...fixture, runId: "ppsspp-cso", formatId: "cso", exampleQuery: "?format=cso" },
      { ...fixture, runId: "ppsspp-iso", formatId: "iso", exampleQuery: "?format=iso" }
    ];
  });
}

export function expandMultiDiscFixtures(manifest) {
  const yabause = manifest.fixtures.find(fixture => fixture.core === "yabause");
  if (!yabause) throw new Error("The yabause baseline fixture is required for multi-disc smoke");
  return (manifest.multiDiscFixtures || [])
    .filter(fixture => fixture.kind === "RUNTIME_POSITIVE")
    .map(fixture => ({
      ...fixture,
      selector: fixture.id,
      runId: fixture.id,
      exampleQuery: `?fixture=${encodeURIComponent(fixture.id)}`,
      bios: yabause.bios,
      coreArtifact: yabause.coreArtifact,
      expectedExternalFiles: fixture.discs.map(disc => disc.localPath)
    }));
}

export function selectableFixtures(manifest, requestedCores = []) {
  const formal = [...manifest.fixtures, ...expandMultiDiscFixtures(manifest)];
  return formal;
}

async function main() {
  const manifest = JSON.parse(await fs.readFile(MANIFEST_PATH, "utf8"));
  const requestedCores = process.argv.slice(2);
  const fixtures = expandFixtureRuns(
    selectableFixtures(manifest, requestedCores),
    requestedCores
  );
  if (!fixtures.length) throw new Error("No core fixtures selected");
  if (fixtures.some(fixture => fixture.core === "dosbox_pure")) await materializeDOSFixtures();

  await fs.mkdir(RESULTS_DIR, { recursive: true });
  const profileDirectory = await fs.mkdtemp(path.join(os.tmpdir(), "retrom-ejs-smoke-"));
  const server = spawn(
    "/usr/bin/python3",
    [path.join(SCRIPT_DIR, "serve.py"), "--port", String(PORT)],
    { cwd: REPO_ROOT, stdio: ["ignore", "pipe", "pipe"] }
  );
  const chromeStderr = [];
  let chrome;
  let cdp;
  const results = [];

  try {
    await waitForServer(server);
    chrome = launchChrome(await resolveChromeBinary(), profileDirectory);
    chrome.stderr.on("data", chunk => {
      chromeStderr.push(chunk.toString("utf8"));
      if (chromeStderr.length > 20) chromeStderr.shift();
    });
    cdp = new CdpPipe(chrome);
    const browserVersion = await cdp.call("Browser.getVersion", {}, undefined, 30000);

    for (const fixture of fixtures) {
      process.stdout.write(`[${fixture.runId || fixture.core}] launching... `);
      const result = await runFixture(cdp, fixture);
      results.push(result);
      if (result.status === "passed") {
        console.log(
          `PASS (${result.durationMs} ms, ${result.smoke.frameDelta} frames, `
          + `${result.visual.colorBuckets} color buckets)`
        );
      } else {
        console.log(`FAIL (${result.failure})`);
      }
    }

    const generatedAtMs = Date.now();
    const report = {
      schemaVersion: 1,
      generatedAtMs,
      emulatorjs: {
        version: manifest.emulatorjs.version,
        runtimeVersions: [...new Set(fixtures.map(fixture => fixture.runtimeVersion || manifest.emulatorjs.version))].sort(),
        archiveSha256: manifest.emulatorjs.archiveSha256
      },
      browser: {
        product: browserVersion.product,
        revision: browserVersion.revision,
        userAgent: browserVersion.userAgent
      },
      criteria: {
        minFrameDelta: 120,
        minColorBuckets: 3,
        minLuminanceStdDev: 5,
        minNonBlackRatio: 0.01,
        manualScreenshotReviewRequired: true
      },
      results
    };
    await fs.writeFile(
      path.join(RESULTS_DIR, "latest.json"),
      `${JSON.stringify(report, null, 2)}\n`,
      "utf8"
    );
    if (results.some(result => result.status !== "passed")) process.exitCode = 1;
  } finally {
    if (cdp) {
      try {
        await cdp.call("Browser.close", {}, undefined, 5000);
      } catch {
        // Chrome may close the pipe before acknowledging Browser.close.
      }
    }
    await stopProcess(chrome);
    await stopProcess(server);
    await fs.rm(profileDirectory, {
      recursive: true,
      force: true,
      maxRetries: 10,
      retryDelay: 200
    });
    if (process.exitCode && chromeStderr.length) {
      console.error(chromeStderr.join("").split("\n").slice(-20).join("\n"));
    }
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch(error => {
    console.error(error.stack || error.message || String(error));
    process.exitCode = 1;
  });
}
