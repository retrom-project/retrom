import {expect, type Page, type TestInfo} from "@playwright/test";
import {runtimeResource, type RuntimeEnvelope} from "./runtime-provider-support";

export async function verifyParentCacheReuse(page: Page, testInfo: TestInfo,
  coldLaunch: () => Promise<void>, warmLaunch: () => Promise<void>) {
  const context = page.context(), requests: string[] = [];
  const configs: Promise<RuntimeEnvelope>[] = [];
  const responses: Array<{status: number; length?: string; etag?: string}> = [];
  const parentPath = "/runtime/content/parent/";
  let warm = false;
  // Routing disables HTTP caching; the second launch cannot fetch parent bytes at all.
  await context.route("**/runtime/content/parent/**", async route => {
    requests.push(route.request().method());
    if (warm) {await route.abort("failed");} else {await route.continue();}
  });
  context.on("response", response => {
    if (response.url().includes(parentPath)) {
      const headers = response.headers();
      responses.push({status: response.status(), length: headers["content-length"], etag: headers.etag});
    }
    if (/\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.ok()) {
      configs.push(response.json() as Promise<RuntimeEnvelope>);
    }
  });
  await coldLaunch();
  expect(requests).toEqual(["GET"]);
  const first = (await Promise.all(configs)).at(-1)!;
  const parent = runtimeResource(first, "parent");
  if (!parent || parent.kind !== "PARENT_ARCHIVE") {throw new Error("parent resource unavailable");}
  expect(responses).toEqual([{status: 200, length: String(parent.sizeBytes), etag: `"sha256-${parent.sha256}"`}]);
  warm = true;
  requests.length = 0;
  // Destroy the document and its runtime/Worker; only durable browser storage survives.
  await page.goto("about:blank");
  await warmLaunch();
  const second = (await Promise.all(configs)).at(-1)!;
  expect(second.session.id).not.toBe(first.session.id);
  expect(runtimeResource(second, "parent")).toEqual(parent);
  expect(requests, "a fresh Launch must reuse parent bytes without HTTP").toEqual([]);
  await testInfo.attach("parent-cache-reuse", {contentType: "application/json", body: JSON.stringify({
    firstLaunch: first.session.id, secondLaunch: second.session.id,
    sizeBytes: parent.sizeBytes, sha256: parent.sha256, coldGets: 1, warmRequests: 0, httpCache: "disabled",
  })});
}
