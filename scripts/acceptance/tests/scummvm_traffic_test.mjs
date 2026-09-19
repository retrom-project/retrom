import {blockPolicy} from "./content_policy_fixture.mjs";
import assert from "node:assert/strict";
import test from "node:test";
import {assertScummvmTraffic, validateScummvmSources} from "../scummvm_traffic.mjs";
const root = `/runtime/content/project/${"a".repeat(64)}/`;
const first = [
  {path: "/runtime/providers/retrom-runtime/bundle/assets/scummvm/plugins/libsky.so", method: "GET", failure: null, sizeBytes: 40, status: 200, range: null},
  {path: root + "sky.dsk", method: "GET", failure: null, sizeBytes: 262144, contentRange: "bytes 0-262143/524288", etag: '"strong"', encoding: null, status: 206, range: "bytes=0-262143"},
];
validateScummvmSources(first, [{url: "sky.dsk", sizeBytes: 524288}], "https://runtime.test" + root + "index.json", blockPolicy);
const restored = [{path: root + "index.json", method: "GET", failure: null, sizeBytes: 40, status: 200, range: null}];
test("ScummVM uses the selected verified whole plugin and caches game blocks across launches", () => {
  assert.equal(assertScummvmTraffic(first, restored, "sky").firstBlockResponses, 1);
});
test("ScummVM cache validation rejects a warm whole GET and any other engine plugin", () => {
  assert.throws(() => assertScummvmTraffic(first, [...restored, {path: root + "sky.dsk", method: "GET", failure: null, sizeBytes: 40, status: 200, range: null}], "sky"));
  assert.throws(() => assertScummvmTraffic([...first, {...first[0], path: first[0].path.replace("libsky", "libscumm")}], restored, "sky"));
});

test("ScummVM rejects cold whole responses, failed transfers and undeclared sources", () => {
  const files = [{url: "sky.dsk", sizeBytes: 524288}];
  validateScummvmSources(first, files, "https://runtime.test" + root + "index.json", blockPolicy);
  for (const change of [{status: 200}, {failure: "net::ERR_FAILED"}, {sizeBytes: 1}, {path: root + "unknown"}]) {
    const changed = [first[0], {...first[1], ...change}];
    assert.throws(() => validateScummvmSources(changed, files, "https://runtime.test" + root + "index.json", blockPolicy));
  }
  assert.throws(() => assertScummvmTraffic([...first, {...first[1], status: 200, range: null}], restored, "sky"));
});
