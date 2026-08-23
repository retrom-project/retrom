import { digestHex } from "./ejs-netplay-4.2.3-v1";

export type SNESStateBlockDigest = {
  tag: string;
  start: number;
  end: number;
  digest: string;
};

const snesStateMagic = new TextEncoder().encode("#!s9xsnp:0012\n");
const blockHeaderBytes = 11;

function matchesAt(value: Uint8Array, expected: Uint8Array, offset: number) {
  return offset + expected.byteLength <= value.byteLength &&
    expected.every((byte, index) => value[offset + index] === byte);
}

function parseBlockHeader(value: Uint8Array, offset: number) {
  if (offset + blockHeaderBytes > value.byteLength || value[offset + 3] !== 0x3a ||
    value[offset + 10] !== 0x3a) {return null;}
  const tagBytes = value.subarray(offset, offset + 3);
  if (!tagBytes.every((byte) => byte >= 0x30 && byte <= 0x5a)) {return null;}
  let length = 0;
  for (let index = offset + 4; index < offset + 10; index += 1) {
    const byte = value[index]!;
    if (byte < 0x30 || byte > 0x39) {return null;}
    length = length * 10 + byte - 0x30;
  }
  const start = offset + blockHeaderBytes;
  const end = start + length;
  if (end > value.byteLength) {return null;}
  return { tag: new TextDecoder().decode(tagBytes), start, end };
}

export async function snesStateBlockDigests(value: Uint8Array) {
  if (!matchesAt(value, snesStateMagic, 0)) {return null;}
  const blocks: SNESStateBlockDigest[] = [];
  let offset = snesStateMagic.byteLength;
  while (offset < value.byteLength) {
    const block = parseBlockHeader(value, offset);
    if (!block) {return null;}
    blocks.push({ ...block, digest: await digestHex(value.subarray(block.start, block.end)) });
    offset = block.end;
  }
  return blocks;
}
