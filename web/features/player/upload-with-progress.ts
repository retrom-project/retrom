export type SaveUploadProgress = {
  loaded: number;
  total: number;
  percent: number;
};

export type UploadRequest = {
  url: string;
  method: "POST" | "PUT";
  headers?: Record<string, string>;
  body: XMLHttpRequestBodyInit;
  totalBytes?: number;
  timeoutMs?: number;
  onProgress: (progress: SaveUploadProgress) => void;
  createRequest?: () => XMLHttpRequest;
};

export type UploadResponse = {
  ok: boolean;
  status: number;
  body: string;
};

const defaultUploadTimeoutMs = 120_000;

function boundedProgress(loaded: number, total: number): SaveUploadProgress {
  const safeTotal = Math.max(1, total);
  const safeLoaded = Math.min(Math.max(0, loaded), safeTotal);
  return {
    loaded: safeLoaded,
    total: safeTotal,
    percent: Math.min(100, Math.floor((safeLoaded / safeTotal) * 100)),
  };
}

export function uploadWithProgress(request: UploadRequest): Promise<UploadResponse> {
  return new Promise((resolve, reject) => {
    const xhr = request.createRequest?.() ?? new XMLHttpRequest();
    const fallbackTotal = Math.max(1, request.totalBytes ?? 1);
    let settled = false;
    const fail = (code = "SAVE_UPLOAD_NETWORK_FAILED") => {
      if (settled) {return;}
      settled = true;
      reject(new Error(code));
    };
    xhr.open(request.method, request.url);
    xhr.timeout = request.timeoutMs ?? defaultUploadTimeoutMs;
    xhr.withCredentials = true;
    for (const [name, value] of Object.entries(request.headers ?? {})) {xhr.setRequestHeader(name, value);}
    xhr.upload.addEventListener("loadstart", () => request.onProgress(boundedProgress(0, fallbackTotal)));
    xhr.upload.addEventListener("progress", (event) => {
      const total = event.lengthComputable && event.total > 0 ? event.total : fallbackTotal;
      request.onProgress(boundedProgress(event.loaded, total));
    });
    xhr.addEventListener("load", () => {
      if (settled) {return;}
      settled = true;
      request.onProgress(boundedProgress(fallbackTotal, fallbackTotal));
      resolve({ ok: xhr.status >= 200 && xhr.status < 300, status: xhr.status, body: xhr.responseText });
    });
    xhr.addEventListener("error", () => fail());
    xhr.addEventListener("abort", () => fail());
    xhr.addEventListener("timeout", () => fail("SAVE_UPLOAD_TIMEOUT"));
    xhr.send(request.body);
  });
}

/** Replays an idempotent save after a short backend restart using the same key and bytes. */
export async function uploadWithRestartRetry(
  request: UploadRequest & {headers: Record<string, string>},
  send: (request: UploadRequest) => Promise<UploadResponse> = uploadWithProgress,
  wait: (ms: number) => Promise<void> = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
): Promise<UploadResponse> {
  if (!request.headers["Idempotency-Key"]) {throw new Error("SAVE_UPLOAD_IDEMPOTENCY_REQUIRED");}
  for (let attempt = 0; attempt < 4; attempt++) {
    try {
      const response = await send(request);
      if (response.ok || ![408, 429, 500, 502, 503, 504].includes(response.status) || attempt === 3) {
        return response;
      }
    } catch (error) {
      if (attempt === 3) {throw error;}
    }
    await wait(1_000 * 2 ** attempt);
  }
  throw new Error("SAVE_UPLOAD_RETRY_EXHAUSTED");
}
