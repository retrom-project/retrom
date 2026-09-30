import assert from "node:assert/strict";
import test from "node:test";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {symbianDebugPanel} from "./symbian_diagnostics.mjs";
import {observeSymbian} from "./symbian_browser.mjs";

test("debug FPS is read from the sibling definition even with other diagnostics present", async () => {
  const browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  try {
    const page = await browser.newPage();
    await page.setContent(`<div class="player-hud-handle">Reveal</div>
      <div class="player-toolbar is-visible"><span class="player-game-meta">Game</span>
      <button aria-label="调试信息">Debug</button></div>
      <aside aria-label="运行调试信息"><dl>
      <div><dt>运行时</dt><dd>RUNNING</dd></div>
      <div><dt>画面呈现率</dt><dd>49.0 FPS</dd></div>
      <div><dt>分辨率</dt><dd>320 × 240</dd></div></dl></aside>`);
    await page.evaluate(() => {
      window.__RETROM_E2E_RUNTIME_V1__ = {getFrameCount: () => Math.round(performance.now())};
    });
    const result = await symbianDebugPanel({page});
    assert.equal(result.fps, 49);
    assert.ok(result.after > result.before);
  } finally {await browser.close();}
});

test("draft evidence observes text node and busy class changes", async () => {
  const browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  try {
    const context = await browser.newContext();
    await observeSymbian(context);
    const page = await context.newPage();
    await page.goto("data:text/html,<span class='player-sync-status'>Pending</span>");
    await page.waitForFunction(() => __symbianSaveStates.length === 1);
    await page.evaluate(() => document.querySelector(".player-sync-status").firstChild.nodeValue = "Staging");
    await page.waitForFunction(() => __symbianSaveStates.at(-1).text === "Staging", null, {timeout: 1_000});
    await page.evaluate(() => document.querySelector(".player-sync-status").classList.add("is-busy"));
    await page.waitForFunction(() => __symbianSaveStates.at(-1).busy, null, {timeout: 1_000});
    const states = await page.evaluate(() => __symbianSaveStates);
    assert.deepEqual(states.map(({text, busy}) => ({text, busy})), [
      {text: "Pending", busy: false}, {text: "Staging", busy: false}, {text: "Staging", busy: true},
    ]);
  } finally {await browser.close();}
});
