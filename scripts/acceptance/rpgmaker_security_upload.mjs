import { createHash, randomUUID } from "node:crypto";
import { closeSync, lstatSync, openSync, readSync, readdirSync, statSync } from "node:fs";
import { basename, join, relative, resolve, sep } from "node:path";

export class SecurityInputBlocked extends Error {}

export function createProductClient(context, baseUrl, csrfToken) {
  const writeHeaders = () => ({
    Origin: baseUrl, "X-Retrom-Csrf": csrfToken, "Idempotency-Key": randomUUID(),
  });

  async function json(method, path, options = {}) {
    const response = await raw(method, path, options);
    if (response.status() !== (options.expected ?? 200)) {
      throw new Error(`RPG_ACCEPTANCE_HTTP_${method}_${response.status()}`);
    }
    return response.json();
  }

  async function raw(method, path, options = {}) {
    return context.request.fetch(`${baseUrl}${path}`, {
      method, headers: options.headers, data: options.data, failOnStatusCode: false,
      timeout: options.timeout,
    });
  }

  async function upload(files, sourceType, purpose = "RPG_MAKER_PROJECT") {
    const totalSizeBytes = files.reduce((total, file) => total + file.sizeBytes, 0);
    const created = await json("POST", "/api/v1/admin/uploads", {
      headers: writeHeaders(), expected: 201,
      data: {
        purpose, sourceType,
        files: files.map((file, index) => ({
          clientFileId: `security-${index}`, relativePath: file.relativePath, sizeBytes: file.sizeBytes,
        })),
      },
    });
    for (let index = 0; index < files.length; index += 1) {
      const remote = created.files.find((file) => file.clientFileId === `security-${index}`);
      if (!remote) { throw new Error("RPG_ACCEPTANCE_SECURITY_UPLOAD_MAPPING_MISSING"); }
      await uploadFile(raw, writeHeaders, created, remote.fileId, files[index]);
    }
    const snapshot = await raw("GET", `/api/v1/admin/uploads/${created.uploadId}`);
    const etag = snapshot.headers().etag;
    if (snapshot.status() !== 200 || !etag) { throw new Error("RPG_ACCEPTANCE_SECURITY_UPLOAD_ETAG_MISSING"); }
    const completed = await json("POST", `/api/v1/admin/uploads/${created.uploadId}/complete`, {
      headers: { ...writeHeaders(), "If-Match": etag }, expected: 202,
    });
    await waitForJob(json, completed.jobId, totalSizeBytes);
    return created.uploadId;
  }

  async function importProject(files, sourceType, platformInstanceId) {
    const uploadId = await upload(files, sourceType);
    const response = await raw("POST", "/api/v1/admin/imports", {
      headers: writeHeaders(), data: {
        uploadId, targetPlatformInstanceId: platformInstanceId,
        metadataProvider: "NONE", contentMode: "RPG_MAKER_PROJECT", tagIds: [],
      },
    });
    const body = await response.json();
    return { status: response.status(), body };
  }

  return { importProject, json, raw, upload, writeHeaders };
}

export function directoryFiles(root, prefix = "") {
  const result = [];
  walk(resolve(root), resolve(root), result, prefix);
  if (!result.length || result.length > 10_000) { throw new Error("RPG_ACCEPTANCE_SECURITY_SOURCE_COUNT"); }
  return result.sort((left, right) => left.relativePath.localeCompare(right.relativePath));
}

export function singleFile(path) {
  const absolute = resolve(path);
  const info = lstatSync(absolute);
  if (!info.isFile() || info.isSymbolicLink()) { throw new Error("RPG_ACCEPTANCE_SECURITY_SOURCE_FILE_INVALID"); }
  return [{ path: absolute, relativePath: basename(absolute), sizeBytes: info.size }];
}

export function mergeFiles(...sets) {
  const merged = new Map();
  for (const files of sets) {
    for (const file of files) {
      if (!merged.has(file.relativePath)) { merged.set(file.relativePath, file); }
    }
  }
  return [...merged.values()].sort((left, right) => left.relativePath.localeCompare(right.relativePath));
}

export function overlayFile(files, path, relativePath) {
  const source = singleFile(path)[0];
  return mergeFiles(files, [{ ...source, relativePath }]);
}

export async function reviewForImport(client, importJobId, options = {}) {
  const attempts = options.attempts ?? 300;
  const waitMs = options.waitMs ?? 100;
  if (!Number.isInteger(attempts) || attempts < 1 || !Number.isInteger(waitMs) || waitMs < 0) {
    throw new Error("RPG_ACCEPTANCE_SECURITY_REVIEW_WAIT_INVALID");
  }
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    const queue = await client.json(
      "GET", `/api/v1/admin/reviews?importJobId=${encodeURIComponent(importJobId)}&limit=20`,
    );
    if (Array.isArray(queue.items) && queue.items.length === 1) {
      return client.json("GET", `/api/v1/admin/reviews/${queue.items[0].itemId}`);
    }
    const job = await client.json("GET", `/api/v1/admin/imports/${importJobId}`);
    requireFreshImportReview(queue, job);
    if (!Array.isArray(queue.items) || queue.items.length > 1) {
      throw new Error("RPG_ACCEPTANCE_SECURITY_REVIEW_CARDINALITY");
    }
    if (attempt + 1 < attempts) {
      await new Promise((resolvePromise) => setTimeout(resolvePromise, waitMs));
    }
  }
  throw new Error("RPG_ACCEPTANCE_SECURITY_REVIEW_CARDINALITY");
}

export function requireFreshImportReview(queue, job) {
  if (Array.isArray(queue?.items) && queue.items.length === 0 &&
      Number.isInteger(job?.alreadyImportedItemCount) && job.alreadyImportedItemCount > 0) {
    throw new SecurityInputBlocked("RPG_ACCEPTANCE_SECURITY_FRESH_DATABASE_REQUIRED");
  }
}

async function uploadFile(raw, writeHeaders, upload, fileId, file) {
  const descriptor = openSync(file.path, "r");
  try {
    for (let start = 0, part = 0; start < file.sizeBytes; start += upload.chunkSizeBytes, part += 1) {
      const length = Math.min(upload.chunkSizeBytes, file.sizeBytes - start);
      const chunk = Buffer.alloc(length);
      if (readSync(descriptor, chunk, 0, length, start) !== length) {
        throw new Error("RPG_ACCEPTANCE_SECURITY_SOURCE_READ_SHORT");
      }
      const digest = createHash("sha256").update(chunk).digest("base64");
      const response = await raw(
        "PUT", `/api/v1/admin/uploads/${upload.uploadId}/files/${fileId}/parts/${part}`,
        { headers: {
          ...writeHeaders(), "Content-Type": "application/octet-stream",
          "Content-Range": `bytes ${start}-${start + length - 1}/${file.sizeBytes}`,
          "Content-Digest": `sha-256=:${digest}:`,
        }, data: chunk },
      );
      if (response.status() !== 204) { throw new Error(`RPG_ACCEPTANCE_SECURITY_UPLOAD_PART_${response.status()}`); }
    }
  } finally { closeSync(descriptor); }
}

function walk(root, directory, result, prefix) {
  for (const name of readdirSync(directory)) {
    const path = join(directory, name);
    const info = lstatSync(path);
    if (info.isSymbolicLink()) { throw new Error("RPG_ACCEPTANCE_SECURITY_SOURCE_SYMLINK"); }
    if (info.isDirectory()) { walk(root, path, result, prefix); }
    else if (info.isFile()) {
      const logical = relative(root, path).split(sep).join("/");
      result.push({ path, relativePath: prefix + logical, sizeBytes: statSync(path).size });
    }
  }
}

export function jobWaitAttemptsForBytes(sizeBytes) {
  return Number.isFinite(sizeBytes) && sizeBytes > 1_073_741_824 ? 6_000 : 600;
}

async function waitForJob(json, jobId, sizeBytes) {
  for (let attempt = 0; attempt < jobWaitAttemptsForBytes(sizeBytes); attempt += 1) {
    const job = await json("GET", `/api/v1/admin/jobs/${jobId}`);
    if (job.state === "SUCCEEDED") { return; }
    if (["FAILED", "CANCELLED"].includes(job.state)) {
      throw new Error(`RPG_ACCEPTANCE_SECURITY_UPLOAD_${job.errorCode ?? job.state}`);
    }
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 100));
  }
  throw new Error("RPG_ACCEPTANCE_SECURITY_UPLOAD_TIMEOUT");
}
