import assert from "node:assert/strict";
import {writeFileSync} from "node:fs";
import {fileURLToPath} from "node:url";
import {resolve} from "node:path";

// Project-owned Lua only; no engine, SDK, game or BIOS bytes are embedded.
export function fantasyControls(core, identity = "canonical-v1") {
  assert.match(identity, /^[a-z0-9.-]{1,64}$/u);
  const marker = `-- retrom fixture ${identity}\n`;
  if (core === "tic80") {
    const code = Buffer.from(`${marker}function BOOT()
 x=pmem(0) if x<20 then x=20 end
 c=pmem(1) if c~=11 then c=8 end
end
function TIC()
 if btn(3) then x=(x+1)%180 end
 if btn(2) then x=(x+179)%180 end
 if btnp(4) then c=11 pmem(0,x) pmem(1,c) end
 cls(0) rect(x,20,5,5,c)
end
`);
    return Buffer.concat([Buffer.from([17, 0, 0, 0, 5, code.length & 255, code.length >> 8, 0]), code]);
  }
  assert.equal(core, "fake08");
  return Buffer.from(`pico-8 cartridge // http://www.pico-8.com
version 42
__lua__
${marker}x=20 c=8
function _update60()
 if btn(1) then x=(x+1)%100 end
 if btn(0) then x=(x+99)%100 end
 if btnp(4) then c=11 end
end
function _draw() cls(0) rectfill(x,20,x+4,24,c) end
`);
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  for (const [core, extension] of [["tic80", "tic"], ["fake08", "p8"]]) {
    writeFileSync(new URL(`./controls.${extension}`, import.meta.url), fantasyControls(core));
  }
}
