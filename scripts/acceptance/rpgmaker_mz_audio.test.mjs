import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {test} from "node:test";
import {runInNewContext} from "node:vm";

test("the licensed MZ fixture plays its real system sound only on a confirm action", () => {
  const recipe = readFileSync(new URL("./rpgmaker_mz_prepare.py", import.meta.url), "utf8");
  const plugin = recipe.match(/PLUGIN_JS = b'''([\s\S]*?)'''/u)?.[1];
  assert.ok(plugin);
  const Scene_Boot = function () {};
  const Scene_Map = function () {};
  Scene_Map.prototype.update = () => {};
  Scene_Map.prototype.createDisplayObjects = () => {};
  let confirmed = false;
  let state = 0;
  const sounds = [];
  runInNewContext(plugin, {
    Scene_Boot, Scene_Map,
    Input: {isTriggered: (key) => key === "ok" && confirmed},
    $gameMap: {width: () => 30}, $gamePlayer: {x: 19, y: 24, locate() {}},
    $gameVariables: {value: () => state, setValue: (_key, value) => {state = value;}},
    SoundManager: {playOk: () => sounds.push("official-ok-sound")},
  });
  const scene = new Scene_Map();
  scene.update();
  assert.deepEqual(sounds, []);
  confirmed = true;
  scene.update();
  assert.equal(state, 1);
  assert.deepEqual(sounds, ["official-ok-sound"]);
});
