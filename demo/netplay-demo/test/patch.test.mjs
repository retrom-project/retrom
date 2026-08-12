import assert from "node:assert/strict";
import test from "node:test";
import { installEmulatorJs423NetplayPatch } from "../patches/ejs-4.2.3-netplay.js";

test("isolates the 4.2.3 save filesystem and keeps other fetches intact", async () => {
  const requested = [];
  const target = {
    clearTimeout,
    console: { log() {} },
    fetch: async (input) => {
      requested.push(input);
      return new Response("ok");
    },
    performance,
    queueMicrotask,
    setTimeout
  };
  const report = installEmulatorJs423NetplayPatch(target);

  let runtimeLog = () => {};
  class GameManager {
    frame = 0;
    state = new Uint8Array([1, 2, 3]);
    mountFileSystems() { throw new Error("IDBFS should not mount"); }
    mkdir(path) { this.paths ??= []; this.paths.push(path); }
    getFrameNum() { return this.frame; }
    getState() { return new Uint8Array(this.state); }
    loadState(state) {
      setTimeout(() => {
        this.state = new Uint8Array(state);
        runtimeLog('[INFO] [State] Loading state "game.state"');
      }, 1);
    }
    toggleMainLoop(active) {
      if (active) setTimeout(() => {
        this.frame += 1;
        target.__RETROM_POST_MAIN_LOOP__?.();
      }, 0);
    }
  }
  target.EJS_GameManager = GameManager;
  let receivedConfig;
  target.EJS_Runtime = async (config) => {
    runtimeLog = target.console.log.bind(target.console);
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
  assert.equal(report.waitableStateLoad, true);
  assert.equal(report.exactFrameStep, true);
  assert.equal(frameHooks, 1);
  const loadResult = await manager.loadStateAndWait(new Uint8Array([9, 8, 7]));
  assert.equal(loadResult.changed, true);
  assert.deepEqual(manager.getState(), new Uint8Array([9, 8, 7]));
  assert.equal(report.provenStateLoads, 1);
  assert.equal(await manager.runNetplayFrame(), 1);
  assert.equal(report.exactFrameSteps, 1);
  const versionResponse = await target.fetch("https://cdn.emulatorjs.org/stable/data/version.json");
  assert.equal((await versionResponse.json()).version, "4.2.3");
  await target.fetch("/game.zip");
  assert.deepEqual(requested, ["/game.zip"]);
});
