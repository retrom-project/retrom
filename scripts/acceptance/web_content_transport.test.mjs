import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {createContext, runInContext} from "node:vm";
import test from "node:test";

const base = new URL("../../internal/httpapi/web_content/", import.meta.url);
function fixture(tyrano = false) {
  const files = new Map([
    ["index.html", {bytes: new TextEncoder().encode('<html><head><base href="old/"><script src="js/main.js"></script></head></html>'), mime: "text/html; charset=utf-8"}],
    ["movies/open.webm", {bytes: Uint8Array.from({length: 700001}, (_, index) => index % 251), mime: "video/webm"}],
    ["data/system/Config.tjs", {bytes: new TextEncoder().encode(';configSave = file\r\n;other = file\n'), mime: "text/plain; charset=utf-8"}],
  ]);
  const reads = [];
  const configuration = {entry: tyrano ? "/__retrom/tyranoscript/entry" : "/__retrom/entry",
    project: tyrano ? "/__retrom/tyranoscript/project/" : "/__retrom/project/", bridge: "/__retrom/bridge.js",
    parent: "https://app.test", tyrano, csp: "script-src 'self'; frame-ancestors https://app.test", permissions: "camera=()", blackJPEG: "AQID"};
  const context = createContext({configuration, Uint8Array, Headers, Response, Request, URL, TextEncoder, TextDecoder,
    ReadableStream, atob, setTimeout, clearTimeout, MessageChannel});
  for (const name of ["rpc.js", "response.js"]) runInContext(readFileSync(new URL(name, base), "utf8"), context);
  context.contentRequest = async request => {
    const file = files.get(request.path);
    if (!file) return {status: 404};
    if (request.type === "STAT") return {status: 200, path: request.path, sizeBytes: file.bytes.length, mediaType: file.mime};
    reads.push(request);
    return {status: 200, bytes: file.bytes.slice(request.offset, request.offset + request.length)};
  };
  return {context, files, reads, configuration};
}
function request(path, options) {return new Request(`https://launch.test${path}`, options);}

test("serves full media and byte ranges through bounded cached reads", async () => {
  const {context, files, reads} = fixture();
  const path = "movies/open.webm", url = `/__retrom/project/${path}`;
  const response = await context.serveContent(request(url), path);
  assert.equal(response.status, 200);
  assert.equal(response.headers.get("Content-Length"), "700001");
  assert.deepEqual(new Uint8Array(await response.arrayBuffer()), files.get(path).bytes);
  assert.ok(reads.length > 1 && reads.every(read => read.length <= 262144));
  for (const [range, expected] of [["bytes=3-17", [3, 18]], ["bytes=-7", [699994, 700001]], ["bytes=699999-", [699999, 700001]]]) {
    const part = await context.serveContent(request(url, {headers: {Range: range}}), path);
    assert.equal(part.status, 206);
    assert.equal(part.headers.get("Content-Range"), `bytes ${expected[0]}-${expected[1] - 1}/700001`);
    assert.deepEqual(new Uint8Array(await part.arrayBuffer()), files.get(path).bytes.slice(...expected));
  }
});
test("HEAD and unsatisfiable ranges do not read content bytes", async () => {
  const {context, reads} = fixture();
  const path = "movies/open.webm", url = `/__retrom/project/${path}`;
  const head = await context.serveContent(request(url, {method: "HEAD"}), path);
  assert.equal(head.status, 200); assert.equal(await head.text(), "");
  for (const range of ["bytes=800000-", "bytes=4-1", "bytes=0-1,3-4", "bytes=-0", "bytes=-9007199254740992", "bytes=0-9007199254740992", "garbage"]) {
    const result = await context.serveContent(request(url, {headers: {Range: range}}), path);
    assert.equal(result.status, 416); assert.equal(result.headers.get("Content-Range"), "bytes */700001");
  }
  assert.equal(reads.length, 0);
});
test("cached entry preserves exact frame policy and puts trusted bridges before game scripts", async () => {
  const {context, configuration} = fixture();
  const response = await context.serveContent(request(configuration.entry), "index.html");
  assert.equal(response.headers.get("Content-Security-Policy"), configuration.csp);
  assert.equal(response.headers.get("Cross-Origin-Resource-Policy"), "cross-origin");
  assert.equal(response.headers.get("Cross-Origin-Embedder-Policy"), "require-corp");
  assert.equal(response.headers.get("Cross-Origin-Opener-Policy"), "same-origin");
  const body = await response.text();
  assert.equal(body.match(/<base /g).length, 1);
  assert.ok(body.indexOf("content-bridge.js") < body.indexOf("js/main.js"));
  assert.ok(body.indexOf('/__retrom/bridge.js') < body.indexOf("js/main.js"));
});
test("Tyrano aliases, config conversion and generated backdrop need no network", async () => {
  const {context, configuration} = fixture(true);
  for (const path of ["/data/system/Config.tjs", "/__retrom/tyranoscript/data/system/Config.tjs", "/__retrom/tyranoscript/project/data/system/Config.tjs"]) {
    assert.equal(context.projectPath(new URL(`https://launch.test${path}`)), "data/system/Config.tjs");
  }
  const config = await context.serveContent(request(`${configuration.project}data/system/Config.tjs?_=123`), "data/system/Config.tjs");
  assert.equal(await config.text(), ';configSave = webstorage\r\n;other = file\n');
  const backdrop = await context.serveContent(request('/data/bgimage/black.jpg'), "data/bgimage/black.jpg");
  assert.deepEqual(new Uint8Array(await backdrop.arrayBuffer()), new Uint8Array([1, 2, 3]));
});
test("unknown paths and methods cannot become network fallbacks or worker scripts", async () => {
  const {context} = fixture();
  assert.equal(context.projectPath(new URL('https://launch.test/api/v1/users')), null);
  const absent = await context.serveContent(request('/__retrom/project/missing.png'), 'missing.png');
  assert.equal(absent.status, 404);
  const worker = request('/__retrom/project/index.html');
  Object.defineProperty(worker, 'destination', {value: 'serviceworker'});
  assert.equal((await context.serveContent(worker, 'index.html')).status, 404);
  assert.equal((await context.serveContent(request('/__retrom/project/index.html', {method: 'POST'}), 'index.html')).status, 404);
});

test("entry projection handles quoted delimiters without corrupting following scripts", () => {
  const {context} = fixture();
  const entry = '<html><head data-test=">"><base href="old/>path"><baseball></baseball><script src="main.js"></script></head></html>';
  const projected = new TextDecoder().decode(context.entryDocument(new TextEncoder().encode(entry)));
  assert.ok(projected.includes('<head data-test=">"><base href="/__retrom/project/">'));
  assert.ok(projected.includes('<baseball></baseball><script src="main.js"></script>'));
  assert.ok(!projected.includes('path"'));
});
