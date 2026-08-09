import { writeHeaders } from "@/lib/api/client";
import { newUuid, sha256 } from "@/lib/crypto";

export type CompletedUpload = {
  uploadId: string;
  uploadFileId: string;
};

type UploadSession = {
  uploadId: string;
  chunkSizeBytes: number;
  files: Array<{ fileId: string; clientFileId: string }>;
};

export async function responseError(response: Response, fallback: string) {
  try {
    const body = await response.json() as { error?: { code?: string; message?: string } };
    return body.error?.message || body.error?.code || fallback;
  } catch {
    return fallback;
  }
}

async function sha256Base64(blob: Blob) {
  const digest = await sha256(await blob.arrayBuffer());
  let binary = "";
  for (const byte of digest) binary += String.fromCharCode(byte);
  return btoa(binary);
}

export async function waitForJob(jobId: string, onProgress?: (message: string) => void) {
  for (let attempt = 0; attempt < 300; attempt += 1) {
    const response = await fetch(`/api/v1/admin/jobs/${jobId}`, { cache: "no-store" });
    if (!response.ok) throw new Error(await responseError(response, "无法读取任务状态"));
    const job = await response.json() as { state: string; phase?: string; errorCode?: string | null };
    onProgress?.(job.phase ? `${job.state} · ${job.phase}` : job.state);
    if (job.state === "SUCCEEDED") return job;
    if (job.state === "FAILED" || job.state === "CANCELLED") throw new Error(job.errorCode ?? `任务${job.state === "FAILED" ? "失败" : "已取消"}`);
    await new Promise((resolve) => window.setTimeout(resolve, 1_000));
  }
  throw new Error("任务在五分钟内没有完成");
}

export async function waitForJobEvents(jobId: string, onProgress?: (eventType: string) => void) {
  return new Promise<void>((resolve, reject) => {
    const source = new EventSource(`/api/v1/admin/jobs/${encodeURIComponent(jobId)}/events`, { withCredentials: true });
    let finished = false;
    let timeout = 0;
    const finish = (error?: Error) => {
      if (finished) return;
      finished = true;
      window.clearTimeout(timeout);
      source.close();
      if (error) reject(error); else resolve();
    };
    timeout = window.setTimeout(() => finish(new Error("任务在五分钟内没有完成")), 300_000);
    const phaseEvents = ["queued", "archive_scanned", "parent_matched", "parent_rejected", "source_snapshot_created", "core_validation_completed"];
    for (const eventType of phaseEvents) source.addEventListener(eventType, () => onProgress?.(eventType));
    source.addEventListener("snapshot", (event) => {
      onProgress?.("snapshot");
      const snapshot = JSON.parse((event as MessageEvent<string>).data) as { state?: string; errorCode?: string | null };
      if (snapshot.state === "SUCCEEDED") finish();
      if (snapshot.state === "FAILED" || snapshot.state === "CANCELLED") finish(new Error(snapshot.errorCode ?? `任务${snapshot.state === "FAILED" ? "失败" : "已取消"}`));
    });
    source.addEventListener("succeeded", () => { onProgress?.("succeeded"); finish(); });
    source.addEventListener("failed", (event) => {
      onProgress?.("failed");
      const details = JSON.parse((event as MessageEvent<string>).data) as { errorCode?: string; code?: string };
      finish(new Error(details.errorCode ?? details.code ?? "任务失败"));
    });
    source.addEventListener("cancelled", () => { onProgress?.("cancelled"); finish(new Error("任务已取消")); });
    // EventSource reconnects automatically. Network errors are deliberately not
    // treated as job cancellation; the bounded timeout is the terminal guard.
  });
}

export async function uploadFiles(files: File[], onProgress?: (message: string) => void): Promise<{ uploadId: string; uploadFileIds: string[] }> {
  if (files.length === 0) throw new Error("至少选择一个文件");
  onProgress?.("正在创建安全上传会话…");
  const create = await fetch("/api/v1/admin/uploads", {
    method: "POST",
    credentials: "same-origin",
    headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }),
    body: JSON.stringify({ sourceType: files.some((file) => file.webkitRelativePath) ? "DIRECTORY" : "FILES", files: files.map((file, index) => ({ clientFileId: `file-${index}`, relativePath: file.webkitRelativePath || file.name, sizeBytes: file.size })) })
  });
  if (!create.ok) throw new Error(await responseError(create, "无法创建上传会话"));
  const session = await create.json() as UploadSession;
  const remoteIDs: string[] = [];
  for (let fileIndex = 0; fileIndex < files.length; fileIndex += 1) {
    const file = files[fileIndex];
    const remote = session.files.find((entry) => entry.clientFileId === `file-${fileIndex}`);
    if (!remote) throw new Error("服务器没有返回上传文件映射");
    remoteIDs.push(remote.fileId);
    for (let offset = 0, part = 0; offset < file.size; offset += session.chunkSizeBytes, part += 1) {
      const end = Math.min(offset + session.chunkSizeBytes, file.size);
      const chunk = file.slice(offset, end);
      onProgress?.(`正在上传 ${fileIndex + 1}/${files.length} · ${Math.round(end / Math.max(file.size, 1) * 100)}%…`);
      const response = await fetch(`/api/v1/admin/uploads/${session.uploadId}/files/${remote.fileId}/parts/${part}`, {
        method: "PUT",
        credentials: "same-origin",
        headers: await writeHeaders({
          "Content-Type": "application/octet-stream",
          "Content-Range": `bytes ${offset}-${end - 1}/${file.size}`,
          "Content-Digest": `sha-256=:${await sha256Base64(chunk)}:`
        }),
        body: chunk
      });
      if (!response.ok) throw new Error(await responseError(response, `文件分块 ${part} 校验失败`));
    }
  }

  const snapshot = await fetch(`/api/v1/admin/uploads/${session.uploadId}`, { cache: "no-store" });
  if (!snapshot.ok) throw new Error(await responseError(snapshot, "无法读取上传状态"));
  onProgress?.("正在校验完整字节并写入内容存储…");
  const completion = await fetch(`/api/v1/admin/uploads/${session.uploadId}/complete`, {
    method: "POST",
    credentials: "same-origin",
    headers: await writeHeaders({ "If-Match": snapshot.headers.get("ETag") ?? "", "Idempotency-Key": newUuid() })
  });
  if (!completion.ok) throw new Error(await responseError(completion, "无法终结上传会话"));
  const result = await completion.json() as { jobId: string };
  await waitForJob(result.jobId, (state) => onProgress?.(`正在终结上传 · ${state}`));
  return { uploadId: session.uploadId, uploadFileIds: remoteIDs };
}

export async function uploadOne(file: File, onProgress?: (message: string) => void): Promise<CompletedUpload> {
  const result = await uploadFiles([file], onProgress);
  return { uploadId: result.uploadId, uploadFileId: result.uploadFileIds[0] };
}
