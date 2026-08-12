import assert from "node:assert/strict";
import test from "node:test";
import { installEmulatorJs423NetplayPatch } from "../patches/ejs-4.2.3-netplay.js";

test("isolates the 4.2.3 save filesystem and keeps other fetches intact", async () => {
  const requested = [];
  const target = {
    fetch: async (input) => {
      requested.push(input);
      return new Response("ok");
    }
  };
  const report = installEmulatorJs423NetplayPatch(target);

  class GameManager {
    mountFileSystems() { throw new Error("IDBFS should not mount"); }
    mkdir(path) { this.paths ??= []; this.paths.push(path); }
  }
  target.EJS_GameManager = GameManager;
  let receivedConfig;
  target.EJS_Runtime = async (config) => {
    receivedConfig = config;
    return {};
  };
  let frameHooks = 0;
  target.__RETROM_POST_MAIN_LOOP__ = () => { frameHooks += 1; };
  await target.EJS_Runtime({ noInitialRun: true });
  receivedConfig.postMainLoop();
  const manager = new target.EJS_GameManager();
  await manager.mountFileSystems();

  assert.deepEqual(manager.paths, ["/data", "/data/saves"]);
  assert.equal(report.inMemorySaves, true);
  assert.equal(report.frameHookBootstrap, true);
  assert.equal(frameHooks, 1);
  const versionResponse = await target.fetch("https://cdn.emulatorjs.org/stable/data/version.json");
  assert.equal((await versionResponse.json()).version, "4.2.3");
  await target.fetch("/game.zip");
  assert.deepEqual(requested, ["/game.zip"]);
});
