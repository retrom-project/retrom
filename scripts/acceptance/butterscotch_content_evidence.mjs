export function trackProjectResponses(page) {
  const entries = [];
  page.on("response", response => {
    if (response.request().method() !== "GET" || !new URL(response.url()).pathname.startsWith("/runtime/content/project/")) {return;}
    const headers = response.headers();
    entries.push({url: response.url(), status: response.status(), bytes: Number(headers["content-length"]),
      range: headers["content-range"] ?? null, requested: response.request().headers().range ?? null});
  });
  return {entries, get urls() {return entries.map(entry => entry.url);}};
}

export function projectReadEvidence(first, restored, previous = first) {
  const data = first.entries.filter(entry => new URL(entry.url).pathname.toLowerCase().endsWith("/data.win"));
  const rangeEntries = first.entries.filter(entry => entry.status === 206);
  const all = [...previous.entries, ...restored.entries];
  for (const entry of all) {
    if (!Number.isSafeInteger(entry.bytes) || entry.bytes <= 0 || ![200,206].includes(entry.status)) {invalid();}
    if (entry.status !== 206) {continue;}
    const match = /^bytes (\d+)-(\d+)\/(\d+)$/u.exec(entry.range ?? "");
    if (!match || entry.requested !== `bytes=${match[1]}-${match[2]}` ||
      Number(match[2]) - Number(match[1]) + 1 !== entry.bytes || Number(match[2]) >= Number(match[3])) {invalid();}
  }
  return {
    dataWinSizeBytes: data.length ? (data[0].range ? Number(data[0].range.split("/")[1]) : data[0].bytes) : 0,
    firstDataWinBytes: data.reduce((total, entry) => total + entry.bytes, 0),
    firstDataWinResponseCount: data.length,
    firstRangeResponseCount: rangeEntries.length,
    largestRangeBytes: Math.max(0, ...all.filter(entry => entry.status === 206).map(entry => entry.bytes)),
    largeWholeResponseCount: all.filter(entry => entry.status === 200 && entry.bytes > 1024 * 1024 &&
      !new URL(entry.url).pathname.endsWith("/index.json")).length,
    restoreDataWinResponseCount: restored.entries.filter(entry => new URL(entry.url).pathname.toLowerCase().endsWith("/data.win")).length,
    restoreIndexResponseCount: restored.entries.filter(entry => new URL(entry.url).pathname.endsWith("/index.json")).length,
    restoreRepeatedBytes: repeatedBytes(previous.entries, restored.entries),
  };
}
function interval(entry) {
  const start = entry.range ? Number(entry.range.split(" ")[1].split("-")[0]) : 0;
  return [start, start + entry.bytes];
}
function repeatedBytes(previous, restored) {
  let repeated = 0;
  for (const entry of restored) {
    const path = new URL(entry.url).pathname;
    if (path.endsWith("/index.json")) {continue;}
    const [start, end] = interval(entry);
    const overlaps = previous.filter(prior => new URL(prior.url).pathname === path).map(interval)
      .map(([from, to]) => [Math.max(start, from), Math.min(end, to)]).filter(([from, to]) => from < to)
      .sort((a, b) => a[0] - b[0]);
    let covered = start;
    for (const [from, to] of overlaps) {
      repeated += Math.max(0, to - Math.max(from, covered)); covered = Math.max(covered, to);
    }
  }
  return repeated;
}
function invalid() {throw new Error("BUTTERSCOTCH_ACCEPTANCE_RANGE_INVALID");}
