import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import {pathToFileURL} from "node:url";
import {isAbsolute, resolve} from "node:path";
// Consumes the actual JSON exported by TestContentIOUnicodeIndexProducer.
// This is an explicit cross-repository integration command, not a synthetic index.
const [runtime, input] = process.argv.slice(2);
assert.ok(isAbsolute(runtime ?? "") && isAbsolute(input ?? ""), "CONTENT_IO_METADATA_INPUT_REQUIRED");
const {boundedJson, indexByteBudget} = await import(pathToFileURL(resolve(runtime, "dist/provider/metadata.js")).href);
const bytes = await readFile(input);
assert.ok(bytes.length > 16 * 1024 * 1024);
await assert.rejects(boundedJson(new Response(bytes), 16 * 1024 * 1024), /METADATA_SIZE_INVALID/);
const value = await boundedJson(new Response(bytes), indexByteBudget(10000, 1024));
assert.equal(value.schemaVersion, 1); assert.equal(value.files.length, 10000);
for (const file of value.files) {
  assert.equal(decodeURIComponent(file.url.split("/").slice(5).join("/")), file.path);
}
console.log(JSON.stringify({caseId: "X-28", level: "INTEGRATION", files: value.files.length, sizeBytes: bytes.length, status: "PASS"}));
