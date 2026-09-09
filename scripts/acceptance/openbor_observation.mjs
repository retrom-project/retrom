import assert from "node:assert/strict";

// Read the real fork's public FS/frame interface. The wrapper forwards every
// factory and callMain argument/result unchanged and never writes game state.
export async function observeOpenBOR(context) {
  await context.addInitScript(() => {
    let factory;
    Object.defineProperty(globalThis, "__RETROM_OPENBOR_MODULE_V1__", {configurable: true,
      get: () => factory, set: (value) => {
        factory = async (options) => {
          const core = await value(options);
          globalThis.__retromOpenBORPixels = () => {
            const copy = document.createElement("canvas"); copy.width = core.canvas.width; copy.height = core.canvas.height;
            const ctx = copy.getContext("2d"); ctx.drawImage(core.canvas, 0, 0);
            return ctx.getImageData(0, 0, copy.width, copy.height);
          };
          let initialSave = null;
          const read = () => {
            let bytes = null, log = "", names = [];
            try {names = core.FS.readdir("/Saves"); bytes = core.FS.readFile("/Saves/game.sav");} catch {}
            try {log = new TextDecoder().decode(core.FS.readFile("/Logs/OpenBorLog.txt"));} catch {}
            let progress = null;
            if (bytes) {
              const text = new TextDecoder("latin1").decode(bytes);
              const offset = text.indexOf("Robo_Rumble\0");
              if (offset >= 4 && offset + 138 <= bytes.length) {
                const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
                progress = {name: "Robo_Rumble", version: view.getUint32(offset - 4, true),
                  level: view.getUint32(offset + 50, true), stage: view.getUint32(offset + 54, true),
                  flag: view.getUint32(offset + 134, true)};
              }
            }
            return {frames: core.retromFrames, paused: core.retromPaused, saveBytes: bytes?.length ?? 0,
              names, progress, levels: [...log.matchAll(/Level Loaded:\s+'([^']+)'/gu)].map(match => match[1])};
          };
          const main = core.callMain;
          core.callMain = (...args) => {initialSave = read(); return main(...args);};
          globalThis.__retromObserveOpenBOR = () => ({...read(), initialSave});
          return core;
        };
      }});
  });
}

export async function nativeState(page) {
  for (const frame of page.frames()) {
    const state = await frame.evaluate(() => globalThis.__retromObserveOpenBOR?.());
    if (state) {return state;}
  }
  return null;
}

export async function menuRow(canvas) {
  const result = await canvas.evaluate(() => {
    const {data, width, height} = globalThis.__retromOpenBORPixels();
    let best = 0, row = 0;
    for (let y = Math.floor(height * 0.45); y < height * 0.8; y++) {
      let count = 0;
      for (let x = Math.floor(width * 0.3); x < width * 0.7; x++) {
        const i = (y * width + x) * 4;
        if (data[i] < 40 && data[i + 1] > 220 && data[i + 2] < 40) {count++;}
      }
      if (count > best) {best = count; row = y / height;}
    }
    return {width, height, best, row};
  });
  console.log("menu pixels", result);
  return result.best > 12 ? result.row : null;
}

export async function spriteX(canvas) {
  return canvas.evaluate(() => {
    const {data, width, height} = globalThis.__retromOpenBORPixels();
    let count = 0, sum = 0;
    for (let y = Math.floor(height * 0.45); y < height * 0.8; y++) {
      for (let x = 0; x < width * 0.55; x++) {
        const i = (y * width + x) * 4;
        if (data[i + 2] > 90 && data[i + 2] > data[i] * 1.6 && data[i + 2] > data[i + 1] * 1.15) {count++; sum += x;}
      }
    }
    return {count, x: count ? sum / count : null};
  });
}

export function assertRobo(state) {
  assert(state?.progress && state.progress.level > 0, "OPENBOR_NATIVE_PROGRESS_MISSING:" + JSON.stringify(state));
  assert.equal(state.progress.version, 0x33750);
  assert(state.levels.length > 0, "OPENBOR_NATIVE_LEVEL_NOT_LOADED");
}
