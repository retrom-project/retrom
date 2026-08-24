type EJSCompressionConstructor = {
  prototype?: {
    getWorkerFile?: (archiveType: string) => Promise<Blob>;
  };
};

declare global {
  interface Window {
    EJS_COMPRESSION?: EJSCompressionConstructor;
  }
}

const normalizedArchiveWorkerReaders = new WeakSet<(...args: never[]) => unknown>();

const archiveWorkerRewrites = {
  "7z": {
    globalLookup: 'eval("_"+_0x222174)',
    safeGlobalLookup: 'Module["_"+_0x222174]',
    dynamicWrapper: "eval(_0x370f8c)",
    safeWrapper: "(function(){return function(){return ccall(_0x405d7e,_0x2bdb59,_0x4f818b,Array.prototype.slice.call(arguments))}})()",
  },
  zip: {
    globalLookup: 'eval("_"+_0x5d9040)',
    safeGlobalLookup: 'Module["_"+_0x5d9040]',
    dynamicWrapper: "eval(_0x6f14b3)",
    safeWrapper: "(function(){return function(){return ccall(_0x557d23,_0x36bd20,_0x501373,Array.prototype.slice.call(arguments))}})()",
  },
} as const;

type RewrittenArchiveType = keyof typeof archiveWorkerRewrites;

function replaceArchiveWorkerFragment(source: string, fragment: string, replacement: string) {
  const first = source.indexOf(fragment);
  if (first < 0 || source.indexOf(fragment, first + fragment.length) >= 0) {
    throw new Error("PLAYER_ARCHIVE_COMPATIBILITY_UNAVAILABLE");
  }
  return `${source.slice(0, first)}${replacement}${source.slice(first + fragment.length)}`;
}

function rewriteArchiveWorker(source: string, archiveType: RewrittenArchiveType) {
  const rewrite = archiveWorkerRewrites[archiveType];
  let rewritten = replaceArchiveWorkerFragment(source, rewrite.globalLookup, rewrite.safeGlobalLookup);
  rewritten = replaceArchiveWorkerFragment(rewritten, rewrite.dynamicWrapper, rewrite.safeWrapper);
  if (rewritten.includes("eval(")) {throw new Error("PLAYER_ARCHIVE_COMPATIBILITY_UNAVAILABLE");}
  return rewritten;
}

function normalizeArchiveWorker(runtimeWindow: typeof window, constructor: EJSCompressionConstructor | undefined) {
  const prototype = constructor?.prototype;
  const original = prototype?.getWorkerFile;
  if (!prototype || typeof original !== "function") {throw new Error("PLAYER_ARCHIVE_COMPATIBILITY_UNAVAILABLE");}
  if (normalizedArchiveWorkerReaders.has(original as (...args: never[]) => unknown)) {return;}
  const normalizedGetWorkerFile = async function (this: unknown, archiveType: string) {
    const worker = await original.call(this, archiveType);
    if (archiveType !== "7z" && archiveType !== "zip") {return worker;}
    const source = await worker.text();
    return new runtimeWindow.Blob([rewriteArchiveWorker(source, archiveType)], {
      type: worker.type || "application/javascript",
    });
  };
  normalizedArchiveWorkerReaders.add(normalizedGetWorkerFile as (...args: never[]) => unknown);
  prototype.getWorkerFile = normalizedGetWorkerFile;
}

function installArchiveWorkerBlobCompatibility(runtimeWindow: typeof window) {
  const previous = Object.getOwnPropertyDescriptor(runtimeWindow, "EJS_COMPRESSION");
  if (previous && !previous.configurable) {
    normalizeArchiveWorker(runtimeWindow, runtimeWindow.EJS_COMPRESSION);
    return () => undefined;
  }
  let current = runtimeWindow.EJS_COMPRESSION;
  if (current) {normalizeArchiveWorker(runtimeWindow, current);}
  Object.defineProperty(runtimeWindow, "EJS_COMPRESSION", {
    configurable: true,
    enumerable: previous?.enumerable ?? true,
    get: () => current,
    set: (value: EJSCompressionConstructor | undefined) => {
      normalizeArchiveWorker(runtimeWindow, value);
      current = value;
    },
  });
  return () => {
    if (current) {
      Object.defineProperty(runtimeWindow, "EJS_COMPRESSION", {
        configurable: true, enumerable: previous?.enumerable ?? true, writable: true, value: current,
      });
    } else if (previous) {
      Object.defineProperty(runtimeWindow, "EJS_COMPRESSION", previous);
    } else {
      Reflect.deleteProperty(runtimeWindow, "EJS_COMPRESSION");
    }
  };
}

function archiveWorkerBaseURL(runtimeWindow: typeof window) {
  if (runtimeWindow.location.protocol === "http:" || runtimeWindow.location.protocol === "https:") {
    return runtimeWindow.location.href;
  }
  const parentLocation = runtimeWindow.parent.location;
  if (parentLocation.protocol === "http:" || parentLocation.protocol === "https:") {return parentLocation.href;}
  throw new Error("PLAYER_ARCHIVE_COMPATIBILITY_UNAVAILABLE");
}

function archiveWorkerRequestURL(runtimeWindow: typeof window, input: RequestInfo | URL, baseURL: string) {
  const value = typeof input === "string"
    ? input
    : input instanceof runtimeWindow.URL
      ? input.href
      : input.url;
  return new runtimeWindow.URL(value, baseURL);
}

function installArchiveWorkerResponseCompatibility(runtimeWindow: typeof window, runtimeBaseUrl: string) {
  const originalFetch = runtimeWindow.fetch;
  if (typeof originalFetch !== "function") {throw new Error("PLAYER_ARCHIVE_COMPATIBILITY_UNAVAILABLE");}
  const baseURL = archiveWorkerBaseURL(runtimeWindow);
  const runtimeURL = new runtimeWindow.URL(runtimeBaseUrl, baseURL);
  const archiveWorkers = new Map<string, RewrittenArchiveType>([
    [new runtimeWindow.URL("compression/extract7z.js", runtimeURL).href, "7z"],
    [new runtimeWindow.URL("compression/extractzip.js", runtimeURL).href, "zip"],
  ]);
  const compatibleFetch: typeof runtimeWindow.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const requestURL = archiveWorkerRequestURL(runtimeWindow, input, baseURL);
    const archiveType = archiveWorkers.get(requestURL.href);
    const response = await originalFetch.call(runtimeWindow, input, init);
    const method = init?.method ?? (typeof input === "string" || "href" in input ? "GET" : input.method);
    if (!archiveType || method.toUpperCase() !== "GET" || !response.ok) {return response;}
    const headers = new runtimeWindow.Headers(response.headers);
    headers.delete("content-length");
    headers.delete("etag");
    const ResponseConstructor = typeof runtimeWindow.Response === "function" ? runtimeWindow.Response : Response;
    return new ResponseConstructor(rewriteArchiveWorker(await response.text(), archiveType), {
      status: response.status, statusText: response.statusText, headers,
    });
  };
  runtimeWindow.fetch = compatibleFetch;
  return () => {
    if (runtimeWindow.fetch === compatibleFetch) {runtimeWindow.fetch = originalFetch;}
  };
}

export function installArchiveWorkerCompatibility(
  runtimeWindow: typeof window,
  emulatorjsVersion: string,
  runtimeBaseUrl: string,
) {
  if (emulatorjsVersion === "4.2.3") {return installArchiveWorkerBlobCompatibility(runtimeWindow);}
  if (emulatorjsVersion === "4.3.0-pre") {
    return installArchiveWorkerResponseCompatibility(runtimeWindow, runtimeBaseUrl);
  }
  return () => undefined;
}
