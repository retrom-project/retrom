import {test} from "node:test";
import assert from "node:assert/strict";
import {classifyHalfMinuteMenu} from "../ppsspp_product_browser.mjs";

test("Half-Minute Hero selection requires the title menu panel, not white text in Mode Select", () => {
  assert.equal(classifyHalfMinuteMenu({darkPanel: true, counts: [52, 0]}), 0);
  assert.equal(classifyHalfMinuteMenu({darkPanel: true, counts: [0, 52]}), 1);
  assert.equal(classifyHalfMinuteMenu({darkPanel: false, counts: [52, 0]}), null);
  assert.equal(classifyHalfMinuteMenu({darkPanel: true, counts: [0, 0]}), null);
});
