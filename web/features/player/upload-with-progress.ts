export type SaveUploadProgress = {
  loaded: number;
  total: number;
  percent: number;
};

type UploadRequest = {
  url: string;
  method: "POST" | "PUT";
  headers?: Record<string, string>;
  body: XMLHttpRequestBodyInit;
  totalBytes?: number;
  onProgress: (progress: SaveUploadProgress) => void;
  createRequest?: () => XMLHttpRequest;
};

export type UploadResponse = {
  ok: boolean;
  status: number;
};

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
    const fail = () => {
      if (settled) return;
      settled = true;
      reject(new Error("SAVE_UPLOAD_NETWORK_FAILED"));
    };
    xhr.open(request.method, request.url);
    xhr.withCredentials = true;
    for (const [name, value] of Object.entries(request.headers ?? {})) xhr.setRequestHeader(name, value);
    xhr.upload.addEventListener("loadstart", () => request.onProgress(boundedProgress(0, fallbackTotal)));
    xhr.upload.addEventListener("progress", (event) => {
      const total = event.lengthComputable && event.total > 0 ? event.total : fallbackTotal;
      request.onProgress(boundedProgress(event.loaded, total));
    });
    xhr.addEventListener("load", () => {
      if (settled) return;
      settled = true;
      request.onProgress(boundedProgress(fallbackTotal, fallbackTotal));
      resolve({ ok: xhr.status >= 200 && xhr.status < 300, status: xhr.status });
    });
    xhr.addEventListener("error", fail);
    xhr.addEventListener("abort", fail);
    xhr.addEventListener("timeout", fail);
    xhr.send(request.body);
  });
}
