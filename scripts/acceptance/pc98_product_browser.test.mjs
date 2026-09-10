import assert from "node:assert/strict";
import test from "node:test";
import {visiblePC98Menu} from "./pc98_product_browser.mjs";

test("menu comparison waits for the blinking selection triangle", async () => {
  const visible = {cursorY: 160, menuSha256: "selected-system"};
  const samples = [{cursorY: null, menuSha256: "hidden-triangle"}, visible];
  let waits = 0;
  const opened = {page: {waitForTimeout: async () => {waits++;}}};
  assert.equal(await visiblePC98Menu(opened, null, null, async () => samples.shift()), visible);
  assert.equal(waits, 1);
});
