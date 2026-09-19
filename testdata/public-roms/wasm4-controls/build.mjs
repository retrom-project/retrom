import {writeFileSync} from "node:fs";
import {pathToFileURL} from "node:url";
// Owned WebAssembly program. State lives in linear memory so checkpoint restore
// is observable without consulting a private runtime API.
const unsigned = value => {
  const bytes = [];
  do {const part = value & 127; value >>>= 7; bytes.push(part | (value ? 128 : 0));} while (value);
  return bytes;
};
const signed = value => {
  const bytes = []; let more;
  do {const part = value & 127; value >>= 7; more = !((value === 0 && !(part & 64)) || (value === -1 && (part & 64)));
    bytes.push(part | (more ? 128 : 0));} while (more);
  return bytes;
};
const string = value => [...unsigned(value.length), ...Buffer.from(value)];
const section = (id, bytes) => [id, ...unsigned(bytes.length), ...bytes];
const constant = value => [0x41, ...signed(value)];
const load = address => [...constant(address), 0x28, 2, 0];
const store = (address, expression) => [...constant(address), ...expression, 0x36, 2, 0];
const whenButton = (mask, body) => [...constant(0x16), 0x2d, 0, 0, ...constant(mask), 0x71, 0x04, 0x40, ...body, 0x0b];
export function build() {
  const code = [0,
    ...whenButton(32, store(12288, [...load(12288), ...constant(1), 0x6a])),
    ...whenButton(16, store(12288, [...load(12288), ...constant(1), 0x6b])),
    ...whenButton(1, store(12292, constant(40))),
    ...constant(0x14), ...constant(2), 0x3b, 1, 0,
    ...load(12288), ...load(12292), ...constant(10), ...constant(10), 0x10, 0, 0x0b];
  return Uint8Array.from([0, 97, 115, 109, 1, 0, 0, 0,
    ...section(1, [2, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 0, 0x60, 0, 0]),
    ...section(2, [2, ...string("env"), ...string("rect"), 0, 0, ...string("env"), ...string("memory"), 2, 1, 1, 1]),
    ...section(3, [1, 1]),
    ...section(7, [1, ...string("update"), 0, 1]),
    ...section(10, [1, ...unsigned(code.length), ...code]),
    ...section(11, [1, 0, ...constant(12288), 0x0b, 8, 20, 0, 0, 0, 20, 0, 0, 0])]);
}
if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url) {
  writeFileSync(new URL("controls.wasm", import.meta.url), build());
}
