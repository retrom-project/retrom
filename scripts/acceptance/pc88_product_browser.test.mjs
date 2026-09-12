import assert from "node:assert/strict";
import {test} from "node:test";
import {isPC88MapReady} from "./pc88_product_browser.mjs";

test("PC-88 waits for the character after the map border appears", () => {
  assert.equal(isPC88MapReady({yellow: 8948, tile: null}), false);
  assert.equal(isPC88MapReady({yellow: 0, tile: "left"}), false);
  assert.equal(isPC88MapReady({yellow: 8948, tile: "left"}), true);
});
