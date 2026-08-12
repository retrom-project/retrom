import assert from "node:assert/strict";
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
    url.searchParams.set("autostart", "1");
    await page.goto(url.href, { waitUntil: "domcontentloaded" });
    await page.waitForFunction(() => {
      const phase = window.__NETPLAY_DEMO_STATUS__?.phase;
      return phase === "running" || phase === "failed";
    }, null, { timeout: 180000 });
    let status = await page.evaluate(() => window.__NETPLAY_DEMO__.getStatus());
    assert.notEqual(status.phase, "failed", status.report.error);

    await page.evaluate(() => {
      window.__NETPLAY_DEMO__.press(0, 3, 300);
      window.__NETPLAY_DEMO__.press(1, 8, 360);
    });
    await page.waitForFunction(() => {
      const phase = window.__NETPLAY_DEMO_STATUS__?.phase;
      return phase === "complete" || phase === "failed";
    }, null, { timeout: 180000 });
    status = await page.evaluate(() => window.__NETPLAY_DEMO__.getStatus());
    const report = status.report;

    assert.equal(status.phase, "complete", report.error);
    assert.equal(report.transport, "websocket");
    assert.equal(report.desyncs, 0);
    assert.ok(report.relay.lastCanonicalFrame >= targetFrame && report.relay.lastCanonicalFrame <= targetFrame + 5);
    assert.ok(report.relay.nonNeutralFrames > 0, "canonical relay did not observe pressed input");
    assert.ok(report.relay.inputTransitions >= 4, "canonical relay did not observe press/release transitions");
    assert.deepEqual(report.clients.map((client) => client.netFrame), [targetFrame, targetFrame]);
    assert.ok(report.clients.every((client) => client.stallCount <= 5), "unexpected lockstep stalls");
    assert.equal(report.hashCheckpoints.at(-1).frame, targetFrame);
    assert.equal(report.hashCheckpoints.at(-1).matched, true);
    assert.deepEqual(consoleErrors, []);

    results.push({
      core,
      frames: targetFrame,
      checkpoints: report.hashCheckpoints.length,
      finalStateDigest: report.hashCheckpoints.at(-1).digest,
      inputTransitions: report.relay.inputTransitions,
      nonNeutralFrames: report.relay.nonNeutralFrames,
      stateSeedMode: report.stateSeedMode,
      initialStateBytes: report.initialStateBytes,
      stateCaptureMs: report.stateCaptureMs,
      stalls: report.clients.map((client) => client.stallCount),
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

console.log(JSON.stringify({ browser: browserVersion, results }, null, 2));
