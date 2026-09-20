import assert from "node:assert/strict";
import {observeContentIO, rangeSummary} from "./content_io_observation.mjs";

const validated = new WeakMap();

export async function trackScummvmTraffic(page, baseUrl) {
  const origin = new URL(baseUrl).origin;
  const observer = observeContentIO(page.context(), value => {
    const url = new URL(value);
    return url.origin === origin && (/\/plugins\//u.test(url.pathname) || url.pathname.startsWith("/runtime/content/project/"));
  });
  await observer.ready;
  const responses = observer.requests;
  responses.stop = () => observer.close();
  responses.validate = async launchId => {
    const configResponse = await page.context().request.get(`${origin}/runtime/launches/${launchId}/config`);
    assert.equal(configResponse.status(), 200);
    const config = await configResponse.json();
    const trees = config.resources.filter(source => source.kind === "FILE_TREE");
    assert.equal(trees.length, 1);
    const indexURL = new URL(trees[0].indexUrl, origin);
    const indexResponse = await page.context().request.get(indexURL.href);
    assert.equal(indexResponse.status(), 200);
    const index = await indexResponse.json();
    await observer.flush();
    validateScummvmSources(responses, index.files, indexURL.href, responses.fetchPolicy);
  };
  page.once("close", responses.stop);
  return responses;
}

export function validateScummvmSources(requests, files, indexURL, policy = requests.fetchPolicy) {
  const indexPath = new URL(indexURL).pathname;
  const sources = files.map(file => ({...file, url: new URL(file.url, indexURL).href}));
  const paths = new Set(sources.map(source => new URL(source.url).pathname));
  for (const item of requests) {
    assert.equal(item.failure, null, "SCUMMVM_TRANSFER_FAILED");
    assert.ok(item.status !== null, "SCUMMVM_RESPONSE_MISSING");
    if (item.path.includes("/plugins/")) continue;
    assert.ok(item.path === indexPath || paths.has(item.path), "SCUMMVM_UNDECLARED_CONTENT");
    if (item.path === indexPath) assert.equal(item.status, 200);
  }
  for (const source of sources) rangeSummary(requests, source, policy);
  for (const item of requests) if (paths.has(item.path)) validated.set(item, JSON.stringify(item));
}

export function assertScummvmTraffic(first, restored, engineId) {
  const body = items => items.filter(item => item.method !== "HEAD");
  for (const item of [...first, ...restored]) {
    assert.equal(item.failure, null, "SCUMMVM_TRANSFER_FAILED");
    assert.ok(item.status !== null, "SCUMMVM_RESPONSE_MISSING");
  }
  const plugins = body(first).filter(item => item.path.includes("/plugins/"));
  assert(plugins.length > 0, "SCUMMVM_PLUGIN_REQUEST_MISSING");
  assert(plugins.every(item => item.path.endsWith(`/lib${engineId}.so`) && item.status === 200 && item.range === null && item.sizeBytes > 0));
  const content = items => body(items).filter(item => item.path.startsWith("/runtime/content/project/"));
  const initial = content(first), resumed = content(restored);
  const blocks = initial.filter(item => !item.path.endsWith("/index.json"));
  assert(blocks.length > 0, "SCUMMVM_LAZY_CONTENT_UNOBSERVED");
  assert.ok(blocks.every(item => validated.get(item) === JSON.stringify(item)), "SCUMMVM_CONTENT_NOT_VALIDATED");
  assert(resumed.some(item => item.path.endsWith("/index.json") && item.status === 200));
  assert.equal(resumed.filter(item => !item.path.endsWith("/index.json")).length, 0, "SCUMMVM_CONTENT_CACHE_NOT_REUSED");
  assert.equal(body(restored).filter(item => item.path.includes("/plugins/")).length, 0, "SCUMMVM_PLUGIN_CACHE_NOT_REUSED");
  const identities = new Set([...initial, ...resumed].map(item => item.path.split("/")[4]));
  assert.equal(identities.size, 1);
  return {engineId, contentDigest: [...identities][0], firstPluginResponses: plugins.length,
    firstBlockResponses: blocks.length, restoredBlockResponses: 0};
}
