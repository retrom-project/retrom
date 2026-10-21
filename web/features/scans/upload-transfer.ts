import { api, ApiError, readError, result, writeHeaders } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";

/** One file chosen in the browser, keyed by its path inside the upload. */
export type UploadEntry = { path: string; file: File };
export type UploadSession = Schema<"Upload">;
export type UploadTotals = { receivedBytes: number; totalBytes: number; completedFiles: number; totalFiles: number };

export const uploadLimits = {
  fileBytes: 4 * 1024 ** 3,
  totalBytes: 20 * 1024 ** 3,
  files: 50_000,
  depth: 24,
};

const parallelFiles = 2;
const retryDelaysMs = [1_000, 2_000, 4_000, 8_000, 16_000];

/** Returns a user-facing reason when the selection exceeds a server limit, else "". */
export function uploadProblem(entries: UploadEntry[]): string {
  if (!entries.length) { return "请选择要上传的文件或文件夹。"; }
  if (entries.length > uploadLimits.files) { return `单次最多上传 ${uploadLimits.files} 个文件。`; }
  const paths = new Set<string>();
  let total = 0;
  for (const entry of entries) {
    if (entry.file.size > uploadLimits.fileBytes) { return `“${entry.path}”超过单文件 4 GiB 上限。`; }
    if (!safePath(entry.path)) { return `“${entry.path}”的路径不能上传。`; }
    if (paths.has(entry.path)) { return `“${entry.path}”重复选择。`; }
    paths.add(entry.path);
    total += entry.file.size;
  }
  for (const path of paths) {
    for (let parent = parentOf(path); parent; parent = parentOf(parent)) {
      if (paths.has(parent)) { return `“${parent}”同时是文件和文件夹。`; }
    }
  }
  return total > uploadLimits.totalBytes ? "单次上传总量不能超过 20 GiB。" : "";
}

function safePath(path: string) {
  const parts = path.split("/");
  return path.length > 0 && path.length <= 1024 && !path.includes("\\") && !path.includes("\0") &&
    parts.length <= uploadLimits.depth + 1 && parts.every((part) => part && part !== "." && part !== "..");
}

function parentOf(path: string) {
  const index = path.lastIndexOf("/");
  return index > 0 ? path.slice(0, index) : "";
}

/** Whether a new selection is exactly the declared file list of an earlier session. */
export function matchesUpload(entries: UploadEntry[], session: UploadSession) {
  const declared = new Map(session.files.map((file) => [file.path, file.sizeBytes]));
  return entries.length === declared.size && entries.every((entry) => declared.get(entry.path) === entry.file.size);
}

/**
 * Continues `resume` when the selection matches it (after a reload or from another browser),
 * otherwise declares a new session. Entries are reordered to the session's file order.
 */
export async function openUpload(
  entries: UploadEntry[], format: UploadSession["format"], resume?: UploadSession,
): Promise<{ session: UploadSession; entries: UploadEntry[] }> {
  const session = resume && matchesUpload(entries, resume)
    ? result(await api.GET("/api/v1/admin/uploads/{uploadId}", { params: { path: { uploadId: resume.id } } }))
    : result(await api.POST("/api/v1/admin/uploads", {
      body: { format, files: entries.map((entry) => ({ path: entry.path, sizeBytes: entry.file.size })) },
    }));
  const byPath = new Map(entries.map((entry) => [entry.path, entry]));
  return { session, entries: session.files.map((file) => byPath.get(file.path)!) };
}

export async function cancelUpload(id: string) {
  const { error, response } = await api.DELETE("/api/v1/admin/uploads/{uploadId}", { params: { path: { uploadId: id } } });
  if (error) { throw new ApiError(error.code, error.message, response.status); }
}

/** Sends every missing byte of the session, two files at a time, each file chunk by chunk. */
export async function transferUpload(
  session: UploadSession,
  entries: UploadEntry[],
  onProgress: (totals: UploadTotals) => void,
  signal: AbortSignal,
): Promise<void> {
  const received = session.files.map((file) => file.receivedBytes);
  const report = () => onProgress({
    receivedBytes: received.reduce((sum, value) => sum + value, 0),
    totalBytes: session.files.reduce((sum, file) => sum + file.sizeBytes, 0),
    completedFiles: session.files.filter((file, index) => received[index] === file.sizeBytes).length,
    totalFiles: session.files.length,
  });
  report();
  const pending = session.files.map((_, index) => index).filter((index) => received[index] < session.files[index].sizeBytes);
  const worker = async () => {
    for (let index = pending.shift(); index !== undefined; index = pending.shift()) {
      await transferFile(session, entries[index].file, index, received, report, signal);
    }
  };
  await Promise.all(Array.from({ length: parallelFiles }, worker));
}

async function transferFile(
  session: UploadSession, file: File, index: number, received: number[], report: () => void, signal: AbortSignal,
) {
  while (received[index] < file.size) {
    const offset = received[index];
    const chunk = file.slice(offset, Math.min(file.size, offset + session.chunkSizeBytes));
    received[index] = await sendChunk(session.id, index, offset, chunk, signal);
    report();
  }
}

/** Sends one chunk with retries; a 409 re-reads the server's offset instead of failing. */
async function sendChunk(id: string, index: number, offset: number, chunk: Blob, signal: AbortSignal): Promise<number> {
  for (let attempt = 0; ; attempt += 1) {
    signal.throwIfAborted();
    let response: Response;
    try {
      response = await fetch(`/api/v1/admin/uploads/${encodeURIComponent(id)}/files/${index}?offset=${offset}`, {
        method: "PUT",
        credentials: "same-origin",
        headers: { ...writeHeaders(), "Content-Type": "application/octet-stream" },
        body: chunk,
        signal,
      });
    } catch (failure) {
      if (signal.aborted || attempt >= retryDelaysMs.length) { throw failure; }
      await wait(retryDelaysMs[attempt], signal);
      continue;
    }
    const payload: unknown = await response.json().catch(() => null);
    if (response.ok) { return (payload as Schema<"UploadProgress">).receivedBytes; }
    if (response.status === 409) { return receivedBytes(id, index); }
    if (response.status < 500 || attempt >= retryDelaysMs.length) {
      const error = readError(payload);
      throw new ApiError(error.code, error.message, response.status);
    }
    await wait(retryDelaysMs[attempt], signal);
  }
}

async function receivedBytes(id: string, index: number) {
  const session = result(await api.GET("/api/v1/admin/uploads/{uploadId}", { params: { path: { uploadId: id } } }));
  return session.files[index].receivedBytes;
}

function wait(milliseconds: number, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    const timer = setTimeout(resolve, milliseconds);
    signal.addEventListener("abort", () => { clearTimeout(timer); reject(signal.reason); }, { once: true });
  });
}
