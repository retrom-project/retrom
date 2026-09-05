import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {test} from "node:test";

test("RPG trial observes audio after the fixture confirm action has played its tone", () => {
  const source = readFileSync(new URL("./rpgmaker_generation_provision.mjs", import.meta.url), "utf8");
  const action = source.indexOf("await advanceFixture(original, config.saveKeys)");
  const audio = source.indexOf("await readAudioObservation(original)");
  const finish = source.indexOf("await finishPreview(original, created.previewId)");
  assert.ok(action >= 0 && action < audio && audio < finish,
    "MV/MZ play the deterministic tone on confirm, not on the silent initial scene");
});
