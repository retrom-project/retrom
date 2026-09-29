import assert from "node:assert/strict";
import test from "node:test";
import {assertNativePreload, assertNativeLocalResponses, assertNativeInput} from "./native_web_cache_contract.mjs";
const files = [{path: "movie.webm", url: "/bytes/movie.webm", sizeBytes: 20}, {path: "empty.json", url: "/bytes/empty.json", sizeBytes: 0}];
const requests = [{path: "/bytes/movie.webm", status: 206, failure: null, sizeBytes: 10}, {path: "/bytes/movie.webm", status: 206, failure: null, sizeBytes: 10}];
const receipt = files.map(file => ({path: file.path, state: "COMPLETE", sizeBytes: file.sizeBytes, committedBytes: file.sizeBytes}));
test("requires exact cold download coverage and durable completion for every file including empty members", () => {
  assertNativePreload(files, requests, receipt);
  assert.throws(() => assertNativePreload(files, requests.slice(1), receipt), /COVERAGE/u);
  assert.throws(() => assertNativePreload(files, [...requests, requests[0]], receipt), /COVERAGE/u);
  assert.throws(() => assertNativePreload(files, requests, receipt.slice(1)), /INCOMPLETE/u);
  assert.throws(() => assertNativePreload(files, requests, [{...receipt[0], state: "PARTIAL"}, receipt[1]]), /INCOMPLETE/u);
  assert.throws(() => assertNativePreload(files, [{...requests[0], status: 200}], receipt), /RANGE_REQUIRED/u);
});
test("allows complete small files but rejects whole downloads of large native media", () => {
  assertNativePreload(files, [{...requests[0], status: 200, sizeBytes: 20}], receipt);
  const large = [{path: "movie.webm", url: "/bytes/movie.webm", sizeBytes: 1048577}];
  const cached = [{path: "movie.webm", state: "COMPLETE", sizeBytes: 1048577, committedBytes: 1048577}];
  assert.throws(() => assertNativePreload(large, [{...requests[0], status: 200, sizeBytes: 1048577}], cached), /RANGE_REQUIRED/u);
});
test("does not confuse a browser resource request with network transfer or a frame with input", () => {
  assertNativeLocalResponses([{local: true, status: 200}]);
  assert.throws(() => assertNativeLocalResponses([{local: false, status: 200}]), /NETWORK_FALLBACK/u);
  assert.throws(() => assertNativeLocalResponses([]), /NETWORK_FALLBACK/u);
  const before = {engine: "MV", map: 1, x: 2};
  assertNativeInput(before, {...before, x: 3});
  assert.throws(() => assertNativeInput(before, {...before}), /DID_NOT_ADVANCE/u);
  assert.throws(() => assertNativeInput(null, before), /UNAVAILABLE/u);
});

test("retains retryable failures without counting their error bodies as game bytes", () => {
  const retry = {...requests[0], status: 503, sizeBytes: 100, failure: "net::ERR_ABORTED"};
  assertNativePreload(files, [retry, ...requests], receipt);
  assert.throws(() => assertNativePreload(files, [retry], receipt), /COVERAGE/u);
  assert.throws(() => assertNativePreload(files, [{...retry, status: 404, failure: null}, ...requests], receipt), /RANGE_REQUIRED/u);
});
