"use client";
import { useState } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { loadUploads, PendingUploads } from "./pending-uploads";
import { UploadSheet, type UploadedSource } from "./upload-sheet";
import type { UploadSession } from "./upload-transfer";

/**
 * Browser uploads on the scan page: the upload sheet, the administrator's unscanned uploads
 * (which survive a reload) and reading a finished upload's collections for mapping.
 */
export function useBrowserUploads({ phone, onUploaded, onFailure, setBusy }: {
  phone: boolean;
  onUploaded: (source: UploadedSource) => void;
  onFailure: (failure: unknown, fallback: string) => void;
  setBusy: (busy: boolean) => void;
}) {
  const [open, setOpen] = useState(false);
  const [resume, setResume] = useState<UploadSession | undefined>();
  const pending = useResource(loadUploads);
  function close() {
    setOpen(false);
    setResume(undefined);
    pending.reload();
  }
  function uploaded(source: UploadedSource) {
    close();
    onUploaded(source);
  }
  async function map(upload: UploadSession) {
    setBusy(true);
    try {
      const data = result(await api.POST("/api/v1/admin/game-scans/inspect", {
        body: { uploadId: upload.id, format: upload.format },
      }));
      uploaded({ uploadId: upload.id, format: upload.format, fileCount: upload.files.length, items: data.items });
    } catch (failure) {
      onFailure(failure, "无法读取已上传的来源集合。");
    } finally {
      setBusy(false);
    }
  }
  const view = <>
    <PendingUploads uploads={pending} canUpload={!phone} onMap={(upload) => void map(upload)}
      onResume={(upload) => { setResume(upload); setOpen(true); }} />
    {open ? <UploadSheet key={resume?.id ?? "new"} resume={resume} onClose={close} onUploaded={uploaded} /> : null}
  </>;
  return { open: () => setOpen(true), reload: pending.reload, view };
}
