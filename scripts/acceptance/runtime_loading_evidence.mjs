const largeFileThresholdBytes = 4 * 1024 * 1024;
const projectPathPattern = /^\/runtime\/content\/project\/([0-9a-f]{64})\/(.+)$/u;

export function trackRuntimeLoading(page, declaredProjectFiles = []) {
  const responses = [];
  const indexes = declaredProjectFiles.length ? [{ files: declaredProjectFiles }] : [];
  const pending = new Set();
  const listener = (response) => {
    if (!trackedUrl(response.url())) {return;}
    const task = recordResponse(response, responses, indexes).finally(() => pending.delete(task));
    pending.add(task);
  };
  page.on("response", listener);
  return {
    snapshot: async () => {
      while (pending.size) {await Promise.allSettled([...pending]);}
      return {
        evidence: summarizeRuntimeLoading({ indexes, responses, timings: await runtimeTimings(page) }),
        projectContentIdentity: singleProjectIdentity(responses),
      };
    },
    stop: () => page.off("response", listener),
  };
}

export function summarizeRuntimeLoading({ indexes, responses, timings }) {
  const files = declaredFiles(indexes);
  const projectResponses = responses.filter(
    (item) => projectFileIdentity(item.url) !== null && item.method !== "HEAD",
  );
  const requestedFiles = new Set(projectResponses.map((item) => item.url));
  const identities = new Set(projectResponses.map((item) => projectFileIdentity(item.url)).filter(Boolean));
  const runtimeResponses = new Set(
    responses.filter((item) => runtimeAsset(item.url)).map((item) => item.url),
  );
  const runtimeEntries = timings.filter((item) => runtimeAsset(item.url));
  return {
    declaredLargeFileCount: [...files.values()].filter((size) => size >= largeFileThresholdBytes).length,
    declaredProjectBytes: [...files.values()].reduce((total, size) => total + size, 0),
    declaredProjectFileCount: files.size,
    fullProjectFileResponseCount: projectResponses.filter((item) => item.status === 200).length,
    nativeProjectResponseCount: responses.filter((item) => nativeProjectAsset(item.url)).length,
    projectContentIdentityCount: identities.size,
    rangeProjectFileResponseCount: projectResponses.filter((item) => item.status === 206).length,
    requestedLargeFileCount: [...requestedFiles].filter(
      (url) => (files.get(url) ?? 0) >= largeFileThresholdBytes,
    ).length,
    requestedProjectBytes: projectResponses.reduce((total, item) => total + item.contentLength, 0),
    requestedProjectFileCount: requestedFiles.size,
    runtimeAssetCacheHitCount: runtimeEntries.filter(
      (item) => item.transferSize === 0 && item.decodedBodySize > 0,
    ).length,
    runtimeAssetRequestCount: runtimeResponses.size,
    runtimeAssetTransferredBytes: runtimeEntries.reduce((total, item) => total + item.transferSize, 0),
  };
}

async function recordResponse(response, responses, indexes) {
  const headers = await response.allHeaders();
  const requestHeaders = await response.request().allHeaders();
  const record = {
    contentLength: unsigned(headers["content-length"]),
    contentRange: headers["content-range"] ?? null,
    method: response.request().method(),
    requestRange: requestHeaders.range ?? null,
    status: response.status(),
    url: response.url(),
  };
  responses.push(record);
  if (!projectIndex(record.url) || record.status !== 200) {return;}
  try {
    const value = await response.json();
    if (!Array.isArray(value?.files)) {return;}
    indexes.push({
      files: value.files.map((file) => ({
        sizeBytes: Number(file?.sizeBytes),
        url: new URL(String(file?.url ?? ""), record.url).href,
      })),
      url: record.url,
    });
  } catch { /* malformed indexes fail through zero declarations */ }
}

function declaredFiles(indexes) {
  const files = new Map();
  for (const index of indexes) {
    for (const file of index.files) {
      if (Number.isSafeInteger(file.sizeBytes) && file.sizeBytes > 0) {files.set(file.url, file.sizeBytes);}
    }
  }
  return files;
}

async function runtimeTimings(page) {
  const values = [];
  for (const frame of page.frames()) {
    const entries = await frame.evaluate(() => performance.getEntriesByType("resource").map((entry) => {
      const resource = entry;
      return {
        decodedBodySize: resource.decodedBodySize ?? 0,
        transferSize: resource.transferSize ?? 0,
        url: resource.name,
      };
    })).catch(() => []);
    values.push(...entries);
  }
  return values;
}

function trackedUrl(value) {
  try {
    const pathname = new URL(value).pathname;
    return pathname.startsWith("/runtime/content/project/") ||
      pathname.startsWith("/runtime/retrom-runtime/") || pathname.startsWith("/__retrom/project/");
  } catch {return false;}
}

function projectFileIdentity(value) {
  try {
    const match = projectPathPattern.exec(new URL(value).pathname);
    return match && match[2] !== "index.json" ? match[1] : null;
  } catch {return null;}
}

function singleProjectIdentity(responses) {
  const identities = new Set(responses.map((item) => projectFileIdentity(item.url)).filter(Boolean));
  return identities.size === 1 ? [...identities][0] : null;
}

function projectIndex(value) {
  try {return projectPathPattern.exec(new URL(value).pathname)?.[2] === "index.json";}
  catch {return false;}
}

function runtimeAsset(value) {
  try {return new URL(value).pathname.startsWith("/runtime/retrom-runtime/");}
  catch {return false;}
}

function nativeProjectAsset(value) {
  try {return new URL(value).pathname.startsWith("/__retrom/project/");}
  catch {return false;}
}

function unsigned(value) {
  if (typeof value !== "string" || !/^(?:0|[1-9][0-9]*)$/u.test(value)) {return 0;}
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : 0;
}
