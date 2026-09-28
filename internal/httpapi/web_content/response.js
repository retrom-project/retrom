function byteRange(header, size) {
  if (header === null) return {offset: 0, length: size, partial: false};
  const match = /^bytes=(\d*)-(\d*)$/.exec(header);
  if (!match || (!match[1] && !match[2]) || size === 0) return null;
  if (match.slice(1).some(value => value && !Number.isSafeInteger(Number(value)))) return null;
  const first = match[1] ? Number(match[1]) : Math.max(0, size - Number(match[2]));
  const last = match[1] && match[2] ? Math.min(size - 1, Number(match[2])) : size - 1;
  if (!Number.isSafeInteger(first) || !Number.isSafeInteger(last) || first < 0 || first >= size || last < first) return null;
  return {offset: first, length: last - first + 1, partial: true};
}
function responseHeaders(mediaType) {
  return new Headers({"Content-Type": mediaType, "X-Content-Type-Options": "nosniff",
    "Cache-Control": "no-store", "Cross-Origin-Resource-Policy": "same-origin", "Accept-Ranges": "bytes"});
}
function entryDocument(bytes) {
  const text = new TextDecoder("utf-8", {fatal: true}).decode(bytes);
  const stripped = text.replace(/<base(?=[\s/>])(?:[^"'<>]|"[^"]*"|'[^']*')*>/gi, "");
  const head = /<head(?=[\s>])(?:[^"'<>]|"[^"]*"|'[^']*')*>/i.exec(stripped);
  if (!head) throw new Error("CONTENT_IO_ENTRY_INVALID");
  const injection = `<base href="${configuration.project}"><script src="/__retrom/content-bridge.js" data-parent="${configuration.parent}"></script><script src="${configuration.bridge}"></script>`;
  const end = head.index + head[0].length;
  return new TextEncoder().encode(stripped.slice(0, end) + injection + stripped.slice(end));
}
function projectPath(url) {
  let value;
  try {value = decodeURIComponent(url.pathname);} catch {return null;}
  if (value === configuration.entry) return "index.html";
  if (value.startsWith(configuration.project)) return value.slice(configuration.project.length);
  if (!configuration.tyrano) return null;
  for (const prefix of ["/__retrom/tyranoscript/", "/"]) {
    const name = value.slice(prefix.length);
    if (value.startsWith(prefix) && (name.startsWith("data/") || name.startsWith("tyrano/"))) return name;
  }
  return null;
}
function validQuery(search) {
  if (!search) return true;
  return configuration.tyrano && /^\?(?:\d{1,20}|_=\d{1,20})(?:&_=\d{1,20})?$/.test(search);
}
async function projectedBytes(path, stat, entry) {
  if (entry) return entryDocument(await readSmall(stat.path, stat.sizeBytes));
  if (configuration.tyrano && path.toLowerCase() === "data/system/config.tjs") {
    const text = new TextDecoder("utf-8", {fatal: true}).decode(await readSmall(stat.path, stat.sizeBytes));
    return new TextEncoder().encode(text.replace(/^([\t ]*;configSave[\t ]*=[\t ]*)file([\t ]*\r?)$/gm, "$1webstorage$2"));
  }
  return null;
}
async function serveContent(request, path) {
  const url = new URL(request.url);
  if (!["GET", "HEAD"].includes(request.method) || !validQuery(url.search) || request.destination === "serviceworker") return new Response(null, {status: 404});
  let stat = await contentRequest({type: "STAT", path});
  let bytes = null;
  if (stat.status === 404 && configuration.tyrano && path.toLowerCase() === "data/bgimage/black.jpg") {
    bytes = Uint8Array.from(atob(configuration.blackJPEG), value => value.charCodeAt(0));
    stat = {status: 200, sizeBytes: bytes.length, mediaType: "image/jpeg"};
  }
  if (stat.status !== 200) return new Response(null, {status: stat.status === 404 ? 404 : 502});
  const entry = url.pathname === configuration.entry;
  bytes ??= await projectedBytes(path, stat, entry);
  const size = bytes?.length ?? stat.sizeBytes;
  const range = byteRange(request.headers.get("Range"), size);
  const headers = responseHeaders(stat.mediaType);
  if (!range) {headers.set("Content-Range", `bytes */${size}`); return new Response(null, {status: 416, headers});}
  headers.set("Content-Length", String(range.length));
  if (range.partial) headers.set("Content-Range", `bytes ${range.offset}-${range.offset + range.length - 1}/${size}`);
  if (entry) {
    headers.set("Content-Security-Policy", configuration.csp);
    headers.set("Cross-Origin-Resource-Policy", "cross-origin");
    headers.set("Cross-Origin-Embedder-Policy", "require-corp");
    headers.set("Cross-Origin-Opener-Policy", "same-origin");
    headers.set("Permissions-Policy", configuration.permissions);
    headers.set("Referrer-Policy", "no-referrer");
  }
  const body = request.method === "HEAD" ? null : bytes ? bytes.slice(range.offset, range.offset + range.length) : contentStream(stat.path, range.offset, range.length);
  return new Response(body, {status: range.partial ? 206 : 200, headers});
}
