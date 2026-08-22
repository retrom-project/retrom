"use client";

import { ConfirmDialog } from "@/components/confirm-dialog";
import { formatBytes } from "@/lib/backend";
import type { PickedDirectory } from "@/lib/directory-access";

type DirectoryPickerDialogProps = {
  browsing: boolean;
  directory: PickedDirectory | null;
  error: string;
  open: boolean;
  onBrowse: () => void;
  onCancel: () => void;
  onConfirm: () => void;
  onDrop: (files: FileList) => void;
};

export function DirectoryPickerDialog({ browsing, directory, error, open, onBrowse, onCancel, onConfirm, onDrop }: DirectoryPickerDialogProps) {
  const files = directory?.files ?? [];
  const totalBytes = files.reduce((total, entry) => total + entry.file.size, 0);
  return <ConfirmDialog
    open={open}
    role="dialog"
    title="选择游戏目录"
    description="先检查目录摘要，再把其中的游戏文件作为一个批次加入导入流程。"
    confirmLabel="使用此目录"
    confirmDisabled={!files.length}
    leadingLabel={files.length ? "重新浏览" : "浏览本机目录"}
    leadingBusy={browsing}
    leadingBusyLabel="正在读取目录…"
    onLeading={onBrowse}
    onCancel={onCancel}
    onConfirm={onConfirm}
    portalToBody
    wide
  >
    <div
      className={`import-directory-dialog-drop${files.length ? " has-selection" : ""}`}
      onDragOver={(event) => event.preventDefault()}
      onDrop={(event) => {event.preventDefault(); onDrop(event.dataTransfer.files);}}
    >
      {files.length ? <>
        <h3>{directory?.name}</h3>
        <p>{files.length} 个文件 · {formatBytes(totalBytes)}</p>
        <ul aria-label="目录文件预览">{files.slice(0, 4).map((entry) => <li key={entry.relativePath}>{entry.relativePath}</li>)}</ul>
        {files.length > 4 ? <small>另有 {files.length - 4} 个文件，将在下一步保留完整相对路径。</small> : null}
      </> : <>
        <strong>将目录拖到这里</strong>
        <p>也可以点击“浏览本机目录”。Chrome / Edge 会直接读取目录；Brave 会退回浏览器目录上传，并显示自身的安全确认。</p>
      </>}
    </div>
    {error ? <div className="feedback bad" role="alert">{error}</div> : null}
  </ConfirmDialog>;
}
