import assert from "node:assert/strict";

// Only the acceptance run's isolated browser storage is reset; login cookies survive.
export async function mameParentCacheProbe(context, origin) {
  const page = await context.newPage(), cdp = await context.newCDPSession(page);
  await cdp.send("Storage.clearDataForOrigin", {origin, storageTypes: "cache_storage,indexeddb,file_systems"});
  await cdp.detach(); await page.close();
  let warm = false, first;
  const requests = [], responses = [];
  const pattern = "**/runtime/content/parent/**";
  await context.route(pattern, async route => {
    requests.push(route.request().method());
    if (warm) {await route.abort("failed");} else {await route.continue();}
  });
  context.on("response", response => {
    if (new URL(response.url()).pathname.startsWith("/runtime/content/parent/")) {
      const headers = response.headers();
      responses.push({status: response.status(), sizeBytes: Number(headers["content-length"]), etag: headers.etag});
    }
  });
  const parent = config => {
    const resource = config.resources.find(resource => resource.role === "parent");
    assert.equal(resource?.kind, "PARENT_ARCHIVE", "MAME_PARENT_CACHE_REQUIRES_SPLIT_SET");
    return resource;
  };
  return {
    cold(config) {
      const resource = parent(config);
      assert.deepEqual(requests, ["GET"], "MAME_PARENT_COLD_DOWNLOAD_COUNT");
      assert.deepEqual(responses, [{status: 200, sizeBytes: resource.sizeBytes, etag: `"sha256-${resource.sha256}"`}]);
      first = {launchId: config.session.id, resource}; warm = true; requests.length = 0;
    },
    restored(config) {
      assert.ok(first, "MAME_PARENT_COLD_LAUNCH_REQUIRED");
      assert.notEqual(config.session.id, first.launchId);
      assert.deepEqual(parent(config), first.resource);
      assert.deepEqual(requests, [], "MAME_PARENT_WARM_NETWORK_FORBIDDEN");
      return {coldLaunch: first.launchId, warmLaunch: config.session.id,
        sizeBytes: first.resource.sizeBytes, sha256: first.resource.sha256,
        coldGets: 1, warmRequests: 0, httpCache: "disabled", warmParentNetwork: "blocked"};
    },
  };
}
