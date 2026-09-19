import test from "node:test";
import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {readFile} from "node:fs/promises";
import {fantasyControls} from "../../../testdata/public-roms/fantasy-controls/build.mjs";

for (const [core, extension, digest] of [
  ["tic80", "tic", "c637c1f3e6f24af56850448fcd6fd6e6c06fa4e40fd735c02582b2cff20fb029"],
  ["fake08", "p8", "9965557a27b806c95174d5aabedb97d166836c26d4ac3372ae2b6bd44c6558f3"],
]) {
  test(`owned ${core} canonical fixture bytes match their sole generation source`, async () => {
    const actual = await readFile(new URL(`../../../testdata/public-roms/fantasy-controls/controls.${extension}`, import.meta.url));
    assert.deepEqual(actual, fantasyControls(core));
    assert.equal(createHash("sha256").update(actual).digest("hex"), digest);
    assert.notDeepEqual(actual, fantasyControls(core, "different-run"));
  });
}
test("owned fantasy generation rejects unknown consumers and invalid comment identities", () => {
  assert.throws(() => fantasyControls("unknown"));
  assert.throws(() => fantasyControls("tic80", "\nfunction TIC() end"));
});
