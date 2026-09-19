import test from "node:test";
import assert from "node:assert/strict";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {focusRuffleSurface} from "./ruffle_product_surface.mjs";

test("Ruffle's late software notice is closed through its UI before focusing the game", {timeout: 15000}, async () => {
  const browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  try {
    const page = await browser.newPage(); await page.setContent('<iframe style="width:600px;height:400px" srcdoc="owned"></iframe><button style="position:fixed;inset:0 0 auto;height:64px;z-index:10">Host HUD</button>');
    const frame = page.frames().find(frame => frame !== page.mainFrame()); assert.ok(frame);
    await frame.setContent(`<canvas style="width:500px;height:300px" onclick="window.focusedGame=true"></canvas>
      <div id="hardware-acceleration-modal" style="position:fixed;inset:0;background:white" onclick="if(event.target===this)this.style.display='none'">
      <button class="close-modal" onclick="this.parentElement.style.display='none'">Close</button></div>`);
    await focusRuffleSurface(page, {frame, canvas: frame.locator("canvas")});
    assert.equal(await frame.locator("#hardware-acceleration-modal").isVisible(), false);
    assert.equal(await frame.evaluate(() => window.focusedGame), true);
  } finally {await browser.close();}
});
