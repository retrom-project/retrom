import {createHash, randomUUID} from "node:crypto";
import {mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync} from "node:fs";
import {join} from "node:path";

export function runCart(bytes, identity) {
  if (!/^[0-9a-f-]{36}$/u.test(identity)) throw new Error("WASM4_RUN_ID_INVALID");
  const name = Buffer.from("retrom-acceptance");
  const body = Buffer.concat([Buffer.from([name.length]), name, Buffer.from(identity)]);
  // An inert custom section changes import identity without changing instructions or memory.
  return Buffer.concat([bytes, Buffer.from([0, body.length]), body]);
}
export async function withWasm4RunCart(fixture, consume) {
  mkdirSync(".cache", {recursive: true});
  const directory = mkdtempSync(".cache/wasm4-acceptance-");
  const path = join(directory, "controls.wasm");
  const original = readFileSync(fixture), bytes = runCart(original, randomUUID());
  const sha = value => createHash("sha256").update(value).digest("hex");
  const receipt = {recipe: "wasm-custom-section-v1", sourceSha256: sha(original), sourceSizeBytes: original.length,
    outputSha256: sha(bytes), outputSizeBytes: bytes.length};
  try {writeFileSync(path, bytes); return await consume(path, bytes, receipt);}
  finally {rmSync(directory, {recursive: true, force: true});}
}
