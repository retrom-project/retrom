import assert from "node:assert/strict";
import test from "node:test";
import {chromium} from "../../web/node_modules/@playwright/test/index.mjs";

test("review selection uses the accessible combobox name despite option label text", async () => {
  const browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  try {
    const page = await browser.newPage();
    await page.setContent('<label>运行版本<select><option value="">请选择要运行的游戏版本</option><option value="sky" selected>Beneath a Steel Sky · DOS</option></select></label>');
    assert.equal(await page.getByLabel("运行版本", {exact: true}).count(), 0);
    assert.equal(await page.getByRole("combobox", {name: /^运行版本/u}).inputValue(), "sky");
  } finally {await browser.close();}
});
