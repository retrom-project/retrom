import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { chromium } from "playwright";
import { demoRoot, readManifest, verifyFile } from "./asset-lib.mjs";
import { startServer } from "./serve.mjs";

const manifest = await readManifest();
for (const asset of manifest.assets) {
  await verifyFile(path.join(demoRoot, asset.target), asset);
}
const targetFrame = Number.parseInt(process.env.NETPLAY_SMOKE_TARGET ?? "3000", 10);
if (![600, 1200, 3000].includes(targetFrame)) {
  throw new Error("NETPLAY_SMOKE_TARGET must be 600, 1200, or 3000");
}

const { server, origin } = await startServer({ port: 0 });
const browser = await chromium.launch({
  executablePath: process.env.CHROME_PATH || chromium.executablePath(),
  headless: true,
  args: [
    "--autoplay-policy=no-user-gesture-required",
    "--enable-unsafe-swiftshader",
    "--use-angle=swiftshader"
  ]
});
const browserVersion = browser.version();

const results = [];
try {
  for (const core of ["nes", "fbneo"]) {
    const page = await browser.newPage();
    const consoleErrors = [];
    page.on("console", (message) => {
      if (message.type() === "error") consoleErrors.push(message.text());
    });
    page.on("pageerror", (error) => consoleErrors.push(error.message));

    const url = new URL(origin);
    url.searchParams.set("core", core);
    url.searchParams.set("transport", "websocket");
    url.searchParams.set("target", String(targetFrame));
    url.searchParams.set("hashEvery", "120");
    url.searchParams.set("latency", "100");
    url.searchParams.set("autostart", "1");
    await page.goto(url.href, { waitUntil: "domcontentloaded" });
    await page.waitForFunction(() => {
      const phase = window.__NETPLAY_DEMO_STATUS__?.phase;
      return phase === "running" || phase === "failed";
    }, null, { timeout: 180000 });
    let status = await page.evaluate(() => window.__NETPLAY_DEMO__.getStatus());
    assert.notEqual(status.phase, "failed", status.report.error);
    assert.deepEqual(status.report.stateLoadEvidence, {
      byteExact: true,
      changed: true,
      nativeCompletion: true,
      stateBytes: status.report.initialStateBytes
    });

    await page.evaluate(() => {
      window.__NETPLAY_DEMO__.press(0, 3, 300);
      window.__NETPLAY_DEMO__.press(1, 8, 360);
    });
    await page.waitForFunction(() => window.__NETPLAY_DEMO__.getStatus().report.clients[0].netFrame >= 60);
    await page.evaluate(() => window.__NETPLAY_DEMO__.dropConnection(1));
    await page.waitForFunction(() => {
      const status = window.__NETPLAY_DEMO__.getStatus();
      return status.phase === "failed" || status.report.relay.reconnects >= 1;
    }, null, { timeout: 15000 });
    status = await page.evaluate(() => window.__NETPLAY_DEMO__.getStatus());
    assert.notEqual(status.phase, "failed", status.report.error);
    await page.waitForFunction(() => window.__NETPLAY_DEMO__.getStatus().report.clients[0].netFrame >= 90);
    await page.evaluate(() => window.__NETPLAY_DEMO__.injectDesync(1, 3, 80));
    await page.waitForFunction(() => {
      const status = window.__NETPLAY_DEMO__.getStatus();
      return status.phase === "failed" || status.report.resyncs >= 1;
    }, null, { timeout: 30000 });
    status = await page.evaluate(() => window.__NETPLAY_DEMO__.getStatus());
    assert.notEqual(status.phase, "failed", status.report.error);
    await page.waitForFunction(() => {
      const phase = window.__NETPLAY_DEMO_STATUS__?.phase;
      return phase === "complete" || phase === "failed";
    }, null, { timeout: 180000 });
    status = await page.evaluate(() => window.__NETPLAY_DEMO__.getStatus());
    const report = status.report;

    assert.equal(status.phase, "complete", report.error);
    assert.equal(report.transport, "websocket");
    assert.ok(report.desyncs >= 1, "fault injection did not create a state mismatch");
    assert.equal(report.resyncs, report.desyncs);
    assert.ok(
      report.resyncLoadEvidence.some((evidence) => evidence.changed && evidence.nativeCompletion),
      "runtime core divergence did not exercise native savestate resync"
    );
    assert.ok(report.reconnectEvents >= 1);
    assert.ok(report.relay.lastCanonicalFrame >= targetFrame && report.relay.lastCanonicalFrame <= targetFrame + 5);
    assert.ok(report.relay.reconnects >= 1);
    assert.ok(report.relay.stateTransfers >= 2, "initial seed and resync were not both acknowledged");
    assert.ok(report.relay.nonNeutralFrames > 0, "canonical relay did not observe pressed input");
    assert.ok(report.relay.inputTransitions >= 4, "canonical relay did not observe press/release transitions");
    assert.deepEqual(report.clients.map((client) => client.netFrame), [targetFrame, targetFrame]);
    assert.ok(report.clients.every((client) => client.stallCount <= 20), "prediction window stalled excessively");
    assert.ok(report.clients.every((client) => client.rollbackCount > 0), "late input did not exercise rollback");
    assert.ok(report.clients.every((client) => client.replay.mutedRuns === client.replay.runs));
    assert.ok(report.clients.every((client) => client.replay.active === false));
    assert.equal(report.hashCheckpoints.at(-1).frame, targetFrame);
    assert.ok(
      report.hashCheckpoints.at(-1).matched || report.hashCheckpoints.at(-1).resynced,
      "final checkpoint neither matched nor completed a byte-verified resync"
    );
    assert.deepEqual(consoleErrors, []);

    results.push({
      core,
      frames: targetFrame,
      checkpoints: report.hashCheckpoints.length,
      finalStateDigest: report.hashCheckpoints.at(-1).digest ?? report.hashCheckpoints.at(-1).resyncDigest,
      inputTransitions: report.relay.inputTransitions,
      nonNeutralFrames: report.relay.nonNeutralFrames,
      stateSeedMode: report.stateSeedMode,
      initialStateBytes: report.initialStateBytes,
      stateCaptureMs: report.stateCaptureMs,
      stalls: report.clients.map((client) => client.stallCount),
      rollbacks: report.clients.map((client) => client.rollbackCount),
      rollbackFrames: report.clients.map((client) => client.rollbackFrames),
      resyncs: report.resyncs,
      resyncFrames: report.resyncEvents.map((event) => event.frame),
      resyncLoadEvidence: report.resyncLoadEvidence,
      reconnects: report.relay.reconnects,
      stateTransfers: report.relay.stateTransfers,
      replayMutedRuns: report.clients.map((client) => client.replay.mutedRuns),
      stateLoadEvidence: report.stateLoadEvidence,
      transport: report.transport
    });
    await page.evaluate(() => window.__NETPLAY_DEMO__.cleanup());
    await page.waitForFunction(() => [...document.querySelectorAll("iframe")]
      .every((frame) => frame.getAttribute("src") === "about:blank"));
    await page.close();
  }
} finally {
  await browser.close();
  await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
}

const evidence = {
  generatedAt: new Date().toISOString(),
  browser: browserVersion,
  emulatorjsVersion: manifest.emulatorjsVersion,
  targetFrame,
  results
};
const evidencePath = path.join(demoRoot, "test-results", "netplay-smoke.json");
await mkdir(path.dirname(evidencePath), { recursive: true });
await writeFile(evidencePath, `${JSON.stringify(evidence, null, 2)}\n`);
console.log(JSON.stringify(evidence, null, 2));
