import {fantasyControls} from "../../testdata/public-roms/fantasy-controls/build.mjs";
import {writeFileSync} from "node:fs";
import {join} from "node:path";

// Owned cartridge source; no downloaded game bytes enter the repository.
export function createFantasyFixture(core, directory, fixtureId = process.env.RETROM_FANTASY_FIXTURE_ID ?? "default") {
  const filename = join(directory, core === "tic80" ? "retrom-checkpoint.tic" : "retrom-checkpoint.p8");
  writeFileSync(filename, fantasyControls(core, fixtureId));
  return filename;
}

export async function spriteState(canvas) {
  return canvas.evaluate((element) => {
    const pixels = element.getContext("2d").getImageData(0, 20, element.width, 1).data;
    const background = element.getContext("2d").getImageData(0, 0, 1, 1).data;
    for (let x = 0; x < element.width; x++) {
      if ([0, 1, 2].some((channel) => pixels[x * 4 + channel] !== background[channel])) {return {x, color: [...pixels.subarray(x * 4, x * 4 + 3)]};}
    }
    throw Error("FANTASY_FIXTURE_SPRITE_MISSING");
  });
}

export async function spritePosition(canvas) {return (await spriteState(canvas)).x;}

// Observe real Web Audio buffers without replacing playback or core exports.
export async function observeFantasyAudio(context) {
  await context.addInitScript(() => {
    globalThis.__retromFantasyAudio = {buffers: 0, nonzeroBuffers: 0};
    const start = AudioBufferSourceNode.prototype.start;
    AudioBufferSourceNode.prototype.start = function (...args) {
      const evidence = globalThis.__retromFantasyAudio;
      evidence.buffers++;
      if (this.buffer && this.buffer.getChannelData(0).some((sample) => Math.abs(sample) > 0.0001)) {
        evidence.nonzeroBuffers++;
      }
      return start.apply(this, args);
    };
  });
}
export async function fantasyAudioEvidence(page) {
  const frames = await Promise.all(page.frames().map((frame) => frame.evaluate(() => globalThis.__retromFantasyAudio)));
  return frames.filter(Boolean).reduce((total, value) => ({buffers: total.buffers + value.buffers,
    nonzeroBuffers: total.nonzeroBuffers + value.nonzeroBuffers}), {buffers: 0, nonzeroBuffers: 0});
}
