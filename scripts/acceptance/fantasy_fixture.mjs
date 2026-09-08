import {writeFileSync} from "node:fs";
import {join} from "node:path";

// Owned cartridge source; no downloaded game bytes enter the repository.
export function createFantasyFixture(core, directory) {
  const fixtureId = process.env.RETROM_FANTASY_FIXTURE_ID ?? "default";
  if (!/^[a-z0-9.-]{1,64}$/u.test(fixtureId)) {throw Error("FANTASY_FIXTURE_ID_INVALID");}
  const marker = `-- retrom fixture ${fixtureId}\n`;
  const filename = join(directory, core === "tic80" ? "retrom-checkpoint.tic" : "retrom-checkpoint.p8");
  if (core === "tic80") {
    const code = Buffer.from(`${marker}function BOOT() x=pmem(0) if x<20 then x=20 end end
function TIC()
 if btn(3) then x=(x+1)%180 end
 if btn(2) then x=(x+179)%180 end
 if btnp(4) then pmem(0,x) end
 cls(0) rect(x,20,5,5,8)
end
`);
    const header = Buffer.from([17,0,0,0,5,code.length & 255,code.length >> 8,0]);
    writeFileSync(filename, Buffer.concat([header, code]));
  } else {
    writeFileSync(filename, `pico-8 cartridge // http://www.pico-8.com
version 42
__lua__
${marker}x=20
function _update60()
 if btn(1) then x=(x+1)%100 end
 if btn(0) then x=(x+99)%100 end
end
function _draw() cls(0) rectfill(x,20,x+4,24,8) end
`);
  }
  return filename;
}

export async function spritePosition(canvas) {
  return canvas.evaluate((element) => {
    const pixels = element.getContext("2d").getImageData(0, 20, element.width, 1).data;
    const background = element.getContext("2d").getImageData(0, 0, 1, 1).data;
    for (let x = 0; x < element.width; x++) {
      if ([0, 1, 2].some((channel) => pixels[x * 4 + channel] !== background[channel])) {return x;}
    }
    throw Error("FANTASY_FIXTURE_SPRITE_MISSING");
  });
}

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
