"use client";

import { useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { newUuid } from "@/lib/crypto";
import { uploadFiles } from "@/lib/upload";
import { formatBytes } from "@/lib/backend";

type ChosenFile = { id: string; file: File; name: string; size: number; path: string };
type Directory = { id: string; name: string; platformName: string; coreName: string };

export function UploadPicker({ directories }: { directories: Directory[] }) {
  const fileInput = useRef<HTMLInputElement>(null);
  const directoryInput = useRef<HTMLInputElement>(null);
  const [files, setFiles] = useState<ChosenFile[]>([]);
  const [target, setTarget] = useState(directories[0]?.id ?? "");
  const [provider, setProvider] = useState("HASHEOUS");
  const [progress, setProgress] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const router = useRouter();

  function choose(list: FileList | null) {
    if (!list) return;
    setFiles(Array.from(list).map((file, index) => ({ id: `f${index + 1}`, file, name: file.name, size: file.size, path: file.webkitRelativePath || file.name })));
  }

  async function upload() {
    setBusy(true); setError("");
    try {
      const uploaded = await uploadFiles(files.map((chosen) => chosen.file), setProgress);
      setProgress("正在创建导入任务…");
      const imported = await fetch("/api/v1/admin/imports", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json", "Idempotency-Key": newUuid() }, body: JSON.stringify({ uploadId: uploaded.uploadId, targetPlatformInstanceId: target, metadataProvider: provider }) });
      if (!imported.ok) throw new Error("上传完成，但无法创建导入任务");
      const result = await imported.json() as { importJobId: string };
      router.push(`/admin/reviews?importJobId=${result.importJobId}`);
      router.refresh();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "上传失败"); setBusy(false); setProgress("");
    }
  }

  return (
    <>
      <div className="dropzone" onDragOver={(event) => event.preventDefault()} onDrop={(event) => { event.preventDefault(); choose(event.dataTransfer.files); }}>
        <div><span aria-hidden="true">⇧</span><h2>将游戏文件或目录拖到这里</h2><p>支持普通 ROM、Arcade ZIP 与 DOS 内容目录；相对路径会完整保留。</p><div className="dropzone-actions"><button className="button" type="button" onClick={() => fileInput.current?.click()}>选择文件</button><button className="button secondary" type="button" onClick={() => directoryInput.current?.click()}>选择目录</button></div></div>
      </div>
      <input ref={fileInput} id="import-files" aria-label="选择导入文件" hidden type="file" multiple onChange={(event) => choose(event.target.files)} />
      <input ref={directoryInput} id="import-directory" aria-label="选择导入目录" hidden type="file" multiple onChange={(event) => choose(event.target.files)} {...{ webkitdirectory: "" }} />
      {files.length ? <div className="panel" style={{ marginTop: 16 }}><div className="panel-head"><div><h2>已选择 {files.length} 个文件</h2><p>合计 {formatBytes(files.reduce((total, file) => total + file.size, 0))}</p></div><button className="button secondary" onClick={() => setFiles([])}>清空</button></div><div className="table-wrap"><table><thead><tr><th>相对路径</th><th>大小</th></tr></thead><tbody>{files.slice(0, 20).map((file) => <tr key={`${file.path}-${file.size}`}><td><strong>{file.path}</strong></td><td title={`${file.size} bytes`}>{formatBytes(file.size)}</td></tr>)}</tbody></table></div></div> : null}
      <section className="panel" style={{ marginTop: 20 }}><div className="panel-head"><div><h2>导入配置</h2><p>配置会冻结到本次任务快照</p></div></div><div className="panel-body form-grid"><div className="field"><label htmlFor="directory">目标平台目录</label><select id="directory" value={target} onChange={(event) => setTarget(event.target.value)}>{directories.map((directory) => <option value={directory.id} key={directory.id}>{directory.name} · {directory.platformName} · {directory.coreName}</option>)}</select></div><div className="field"><label htmlFor="provider">元信息来源</label><select id="provider" value={provider} onChange={(event) => setProvider(event.target.value)}><option value="HASHEOUS">Hasheous 哈希查询</option><option value="NONE">不刮削</option></select></div><div className="field full"><button className="button" disabled={busy || files.length === 0 || !target} onClick={() => void upload()}>{busy ? progress : "上传、验证并创建导入任务"}</button>{error ? <p role="alert" className="status bad">{error}</p> : null}</div></div></section>
    </>
  );
}
