import assert from "node:assert/strict";

// Observe the real module's exported FS/frame/audio interface. All arguments,
// return values and native game files remain unchanged. Fetches used to verify
// asset hashes receive the original bytes; only the imported factory is wrapped.
export async function observeNXEngine(context) {
  await context.route("**/nxengine-retrom.mjs", async route => {
    if (route.request().resourceType() !== "script") return route.continue();
    const response = await route.fetch(), source = await response.text();
    assert(source.includes("export default Module;"), "NXENGINE_OBSERVER_ABI");
    const wrapper = `export default async (...args) => {
      const core = await Module(...args);
      let frames = 0, nonzeroAudio = 0, initial = null;
      const read = () => {
        let saves = [];
        try {
          saves = core.FS.readdir('/save').filter(n => /^profile[2-5]?\\.dat$/.test(n))
            .map(name => ({name, bytes: Array.from(core.FS.readFile('/save/' + name))}));
        } catch {}
        return {frames, nonzeroAudio, saves};
      };
      const load = core._retrom_load, step = core._retrom_step;
      core._retrom_load = (...a) => {initial = read(); return load(...a);};
      core._retrom_step = (...a) => {
        const result = step(...a); frames++;
        const offset = core._retrom_audio() / 2, count = core._retrom_audio_count();
        if (core.HEAP16.subarray(offset, offset + count).some(value => value !== 0)) nonzeroAudio++;
        return result;
      };
      globalThis.__retromObserveNXEngine = () => ({...read(), initial});
      return core;
    };`;
    return route.fulfill({response, body: source.replace("export default Module;", wrapper)});
  });
}
export async function nativeState(page) {
  for (const frame of page.frames()) {
    const state = await frame.evaluate(() => globalThis.__retromObserveNXEngine?.());
    if (state) return state;
  }
  return null;
}
export async function pad(page, button, duration = 160, delay = 500) {
  for (const pressed of [true, false]) {
    await Promise.all(page.frames().map(frame => frame.evaluate(({button, pressed}) => {
      globalThis.__retromTestGamepad?.button(button, pressed);
    }, {button, pressed})));
    await page.waitForTimeout(pressed ? duration : delay);
  }
}
