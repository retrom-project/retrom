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
  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [showFiles, setShowFiles] = useState(false);
  const [target, setTarget] = useState("");
  const [provider, setProvider] = useState("HASHEOUS");
  const [progress, setProgress] = useState("");
  const [completedJobId, setCompletedJobId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const router = useRouter();

  function choose(list: FileList | null) {
    if (!list) return;
    setFiles(Array.from(list).map((file, index) => ({ id: `f${index + 1}`, file, name: file.name, size: file.size, path: file.webkitRelativePath || file.name })));
    setShowFiles(false);
    setCompletedJobId("");
    setError("");
    setStep(1);
  }

  async function upload() {
    setBusy(true); setError(""); setCompletedJobId(""); setStep(3);
    try {
      const uploaded = await uploadFiles(files.map((chosen) => chosen.file), setProgress);
      setProgress("正在创建导入任务…");
      const imported = await fetch("/api/v1/admin/imports", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json", "Idempotency-Key": newUuid() }, body: JSON.stringify({ uploadId: uploaded.uploadId, targetPlatformInstanceId: target, metadataProvider: provider }) });
      if (!imported.ok) throw new Error("上传完成，但无法创建导入任务");
      const result = await imported.json() as { importJobId: string };
      setCompletedJobId(result.importJobId);
      setProgress("导入任务已创建，后台会继续识别游戏、检查运行依赖并准备游戏信息。");
      setBusy(false);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "上传失败"); setBusy(false); setProgress("");
    }
  }

  const selectedDirectory = directories.find((directory) => directory.id === target);
  const totalBytes = files.reduce((total, file) => total + file.size, 0);
  const uploadPercent = completedJobId ? 100 : /创建导入任务/.test(progress) ? 92 : /终结|校验|内容存储/.test(progress) ? 76 : /上传/.test(progress) ? 48 : busy ? 12 : 0;

  return (
    <div className="import-wizard">
      <div className="stepper import-wizard-steps" aria-label="导入步骤">{([[1, "选择内容"], [2, "确认配置"], [3, "上传并验证"]] as const).map(([number, label]) => <div className={`step${step === number ? " is-active" : step > number ? " is-complete" : ""}`} key={number}><i>{step > number ? "✓" : number}</i>{label}</div>)}</div>
      <input ref={fileInput} id="import-files" aria-label="选择导入文件" hidden type="file" multiple onChange={(event) => choose(event.target.files)} />
      <input ref={directoryInput} id="import-directory" aria-label="选择导入目录" hidden type="file" multiple onChange={(event) => choose(event.target.files)} {...{ webkitdirectory: "" }} />
      {step === 1 ? <section className="import-wizard-stage">
        <div className="dropzone import-dropzone" onDragOver={(event) => event.preventDefault()} onDrop={(event) => { event.preventDefault(); choose(event.dataTransfer.files); }}><div><span aria-hidden="true">⇧</span><h2>将游戏文件或目录拖到这里</h2><p>支持普通 ROM、Arcade ZIP 与 DOS 内容目录；相对路径会完整保留。</p><div className="dropzone-actions"><button className="button" type="button" onClick={() => fileInput.current?.click()}>选择文件</button><button className="button secondary" type="button" onClick={() => directoryInput.current?.click()}>选择目录</button></div></div></div>
        {files.length ? <><div className="import-selected-summary"><div><span className="status good"><i />已选择</span><h3>{files.length} 个文件 · {formatBytes(totalBytes)}</h3><p>文件相对路径会完整保留，上传前仍可重新选择。</p></div><div><button className="button secondary" type="button" onClick={() => setShowFiles((current) => !current)}>{showFiles ? "收起文件清单" : "查看文件清单"}</button><button className="button secondary" type="button" onClick={() => { setFiles([]); setShowFiles(false); }}>重新选择</button><button className="button" type="button" onClick={() => setStep(2)}>下一步</button></div></div>{showFiles ? <section className="panel import-file-preview"><div className="panel-head"><div><h2>已选择文件</h2><p>当前展示前 20 项，共 {files.length} 个文件。</p></div></div><div className="table-wrap"><table><thead><tr><th>相对路径</th><th>类型</th><th>大小</th></tr></thead><tbody>{files.slice(0, 20).map((file) => <tr key={`${file.path}-${file.size}`}><td><strong>{file.path}</strong></td><td>{file.name.toLocaleLowerCase().endsWith(".zip") ? "ZIP 压缩包" : "游戏文件"}</td><td title={`${file.size} bytes`}>{formatBytes(file.size)}</td></tr>)}</tbody></table></div></section> : null}</> : null}
      </section> : null}
      {step === 2 ? <section className="panel import-config-panel"><div className="panel-head"><div><h2>确认导入配置</h2><p>目标目录决定基础平台和推荐运行方式；配置会冻结到本次任务快照。</p></div><span className="status info"><i />步骤 2 / 3</span></div><div className="panel-body"><div className="form-grid import-config-grid"><div className="field"><label htmlFor="directory">目标游戏目录</label><select id="directory" value={target} onChange={(event) => setTarget(event.target.value)}><option value="" disabled>{directories.length ? "请选择目标游戏目录" : "暂无可用游戏目录"}</option>{directories.map((directory) => <option value={directory.id} key={directory.id}>{directory.name}</option>)}</select><small>必须主动选择，避免将游戏导入到错误目录。</small></div><div className="field"><label htmlFor="provider">元信息来源</label><select id="provider" value={provider} onChange={(event) => setProvider(event.target.value)}><option value="HASHEOUS">Hasheous 哈希查询</option><option value="NONE">不刮削</option></select></div><div className="field"><label>游戏平台</label><input value={selectedDirectory?.platformName ?? "选择目录后显示"} disabled /></div><div className="field"><label>推荐运行方式</label><input value={selectedDirectory?.coreName ?? "选择目录后显示"} disabled /></div></div><div className="import-config-summary"><div><small>内容</small><strong>{files.length} 个文件</strong></div><div><small>数据量</small><strong>{formatBytes(totalBytes)}</strong></div><div><small>目标</small><strong>{selectedDirectory?.name ?? "尚未选择"}</strong></div><div><small>运行方式</small><strong>{selectedDirectory?.coreName ?? "尚未确定"}</strong></div></div><div className="import-stage-actions"><button className="button secondary" type="button" onClick={() => setStep(1)}>上一步</button><button className="button" type="button" disabled={busy || !target} onClick={() => void upload()}>开始上传并验证</button></div></div></section> : null}
      {step === 3 ? <section className="panel import-progress-card"><StatusBadgeLike completed={Boolean(completedJobId)} busy={busy} /><h2>{completedJobId ? "导入任务已创建" : error ? "上传没有完成" : "正在上传并验证"}</h2><p>{error || progress || "正在准备安全上传会话…"}</p><div className="import-progress-track"><i style={{ width: `${uploadPercent}%` }} /></div><div className="import-progress-line"><span>{completedJobId ? "任务已进入后台处理" : progress || "准备上传…"}</span><strong>{uploadPercent}%</strong></div><div className="import-progress-steps"><div><strong>上传文件</strong><span>{uploadPercent >= 76 ? "完成" : busy ? "处理中" : error ? "未完成" : "等待"}</span></div><div><strong>完整性校验</strong><span>{uploadPercent >= 92 ? "完成" : uploadPercent >= 76 ? "处理中" : "等待"}</span></div><div><strong>创建导入任务</strong><span>{completedJobId ? "完成" : uploadPercent >= 92 ? "处理中" : "等待"}</span></div></div><div className="import-stage-actions"><span>{completedJobId ? "后台会继续识别、运行检查和游戏信息准备。" : "上传过程中可以离开页面；已创建的后台任务会继续运行。"}</span>{completedJobId ? <button className="button" type="button" onClick={() => { router.push("/admin/imports/tasks"); router.refresh(); }}>查看任务进度 →</button> : error ? <button className="button secondary" type="button" onClick={() => setStep(2)}>返回配置</button> : null}</div></section> : null}
    </div>
  );
}

function StatusBadgeLike({ completed, busy }: { completed: boolean; busy: boolean }) {
  return <span className={`status ${completed ? "good" : busy ? "info" : "bad"}`}><i />{completed ? "已完成" : busy ? "正在处理" : "需要处理"}</span>;
}
