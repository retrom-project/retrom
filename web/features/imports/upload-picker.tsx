"use client";

import Link from "next/link";
import { useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { newUuid } from "@/lib/crypto";
import { uploadFiles } from "@/lib/upload";
import { formatBytes } from "@/lib/backend";
import { writeHeaders } from "@/lib/api/client";
import type { ImportDetail } from "./import-workflow";
import { preflightMultiDisc, type MultiDiscPreflight } from "./multidisc-preflight";

type ChosenFile = { id: string; file: File; name: string; size: number; path: string };
type Directory = {
  id: string; name: string; platformName: string; coreName: string;
  importCapabilities?: { contentModes: string[]; multiDisc: { maxDiscs: number; maxTotalBytes: number } | null };
};

export function UploadPicker({ directories, reconfigureSource = null }: { directories: Directory[]; reconfigureSource?: ImportDetail | null }) {
  const fileInput = useRef<HTMLInputElement>(null);
  const directoryInput = useRef<HTMLInputElement>(null);
  const [files, setFiles] = useState<ChosenFile[]>([]);
  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [showFiles, setShowFiles] = useState(false);
  const reusableFiles = reconfigureSource?.fileOutcomes.filter((file) => file.disposition === "REJECTED" && !file.resolution) ?? [];
  const [target, setTarget] = useState(reconfigureSource?.targetPlatformInstance.id ?? "");
  const [provider, setProvider] = useState(reconfigureSource?.metadataProvider ?? "HASHEOUS");
  const [contentMode, setContentMode] = useState<"STANDARD" | "MULTI_DISC_M3U_V1">("STANDARD");
  const [preflight, setPreflight] = useState<MultiDiscPreflight | null>(null);
  const [preflighting, setPreflighting] = useState(false);
  const [progress, setProgress] = useState("");
  const [completedJobId, setCompletedJobId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const router = useRouter();

  function choose(list: FileList | null) {
    if (!list) return;
    const chosen = Array.from(list).map((file, index) => ({ id: `f${index + 1}`, file, name: file.name, size: file.size, path: file.webkitRelativePath || file.name }));
    setFiles(chosen);
    setPreflighting(true);
    void preflightMultiDisc(chosen).then((result) => {
      setPreflight(result);
      setContentMode(result.detected ? "MULTI_DISC_M3U_V1" : "STANDARD");
    }).catch(() => setPreflight(null)).finally(() => setPreflighting(false));
    setShowFiles(false);
    setCompletedJobId("");
    setError("");
    setStep(1);
  }

  async function submitImport() {
    setBusy(true); setError(""); setCompletedJobId(""); setStep(3);
    try {
      let imported: Response;
      if (reconfigureSource) {
        setProgress("正在复用已上传文件并按新配置重新识别…");
        imported = await fetch(`/api/v1/admin/imports/${reconfigureSource.importJobId}/reconfigure`, {
          method: "POST",
          credentials: "same-origin",
          headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${reconfigureSource.version}"`, "Idempotency-Key": newUuid() }),
          body: JSON.stringify({ targetPlatformInstanceId: target, metadataProvider: provider }),
        });
      } else {
        const uploaded = await uploadFiles(files.map((chosen) => chosen.file), setProgress);
        setProgress("正在创建导入任务…");
        imported = await fetch("/api/v1/admin/imports", { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }), body: JSON.stringify({ uploadId: uploaded.uploadId, targetPlatformInstanceId: target, metadataProvider: provider, contentMode }) });
      }
      if (!imported.ok) throw new Error(reconfigureSource ? "无法按新配置创建导入任务，请刷新任务后重试" : "上传完成，但无法创建导入任务");
      const result = await imported.json() as { importJobId: string };
      setCompletedJobId(result.importJobId);
      setProgress("导入任务已创建，后台会继续识别游戏、检查运行依赖并准备游戏信息。");
      setBusy(false);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "上传失败"); setBusy(false); setProgress("");
    }
  }

  const selectedDirectory = directories.find((directory) => directory.id === target);
  const multiDiscSupported = selectedDirectory?.importCapabilities?.contentModes.includes("MULTI_DISC_M3U_V1") ?? false;
  const multiDiscInvalid = contentMode === "MULTI_DISC_M3U_V1" &&
    (!preflight?.detected || !multiDiscSupported || preflight.completeGroupCount + preflight.blockedGroupCount === 0);
  const fileCount = reconfigureSource ? reusableFiles.length : files.length;
  const totalBytes = reconfigureSource ? reusableFiles.reduce((total, file) => total + file.sizeBytes, 0) : files.reduce((total, file) => total + file.size, 0);
  const uploadPercent = completedJobId ? 100 : /创建导入任务/.test(progress) ? 92 : /终结|校验|内容存储/.test(progress) ? 76 : /上传/.test(progress) ? 48 : busy ? 12 : 0;

  return (
    <div className="import-wizard">
      <div className="stepper import-wizard-steps" aria-label="导入步骤">{([[1, reconfigureSource ? "复用内容" : "选择内容"], [2, "确认配置"], [3, reconfigureSource ? "重新识别" : "上传并验证"]] as const).map(([number, label]) => <div className={`step${step === number ? " is-active" : step > number ? " is-complete" : ""}`} key={number}><i>{step > number ? "✓" : number}</i>{label}</div>)}</div>
      <input ref={fileInput} id="import-files" aria-label="选择导入文件" hidden type="file" multiple onChange={(event) => choose(event.target.files)} />
      <input ref={directoryInput} id="import-directory" aria-label="选择导入目录" hidden type="file" multiple onChange={(event) => choose(event.target.files)} {...{ webkitdirectory: "" }} />
      {step === 1 ? <section className="import-wizard-stage">
        {reconfigureSource ? <div className="dropzone import-dropzone import-reuse-zone"><div><span aria-hidden="true">↻</span><h2>复用服务器中已上传的内容</h2><p>将重新处理原任务中尚未解决的 {reusableFiles.length} 个文件，不会再次上传文件内容。</p><div className="dropzone-actions"><button className="button" type="button" disabled={!reusableFiles.length} onClick={() => setStep(2)}>重新选择平台目录</button><Link className="button secondary" href="/admin/imports/new">改为上传新文件</Link></div></div></div> : <div className="dropzone import-dropzone" onDragOver={(event) => event.preventDefault()} onDrop={(event) => { event.preventDefault(); choose(event.dataTransfer.files); }}><div><span aria-hidden="true">⇧</span><h2>将游戏文件或目录拖到这里</h2><p>支持普通 ROM、Arcade ZIP、DOS 内容目录与多盘 M3U + CHD；相对路径会完整保留。</p><div className="dropzone-actions"><button className="button" type="button" onClick={() => fileInput.current?.click()}>选择文件</button><button className="button secondary" type="button" onClick={() => directoryInput.current?.click()}>选择目录</button></div></div></div>}
        {preflighting ? <div className="feedback info" role="status">正在读取 M3U…</div> : preflight?.detected ? <div className={`feedback ${preflight.rejectedGroupCount ? "bad" : preflight.missingDiscCount ? "warn" : "good"}`}><strong>{preflight.missingDiscCount ? `目录缺少 ${preflight.missingDiscCount} 张光盘` : preflight.rejectedGroupCount ? "部分多盘目录无法处理" : "目录完整，可以上传"}</strong><p>发现 {preflight.groups.length} 个 M3U 分组：完整 {preflight.completeGroupCount}、待补盘 {preflight.blockedGroupCount}、拒绝 {preflight.rejectedGroupCount}；另有 {preflight.ignoredFileCount} 个未引用文件会忽略。</p>{preflight.groups.some((group) => group.missing.length) ? <ul>{preflight.groups.filter((group) => group.missing.length).map((group) => <li key={group.directory}>{group.directory}：缺少 {group.missing.join("、")}</li>)}</ul> : null}</div> : null}
        {reconfigureSource && reusableFiles.length ? <><div className="import-selected-summary"><div><span className="status good"><i />服务器文件可复用</span><h3>{reusableFiles.length} 个文件 · {formatBytes(totalBytes)}</h3><p>原始失败记录会保留；新任务创建后，旧任务不再把这些文件计为待处理异常。</p></div><div><button className="button secondary" type="button" onClick={() => setShowFiles((current) => !current)}>{showFiles ? "收起文件清单" : "查看文件清单"}</button><button className="button" type="button" onClick={() => setStep(2)}>下一步</button></div></div>{showFiles ? <section className="panel import-file-preview"><div className="panel-head"><div><h2>将复用的文件</h2><p>当前展示前 20 项，共 {reusableFiles.length} 个文件。</p></div></div><div className="table-wrap"><table><thead><tr><th>相对路径</th><th>原失败原因</th><th>大小</th></tr></thead><tbody>{reusableFiles.slice(0, 20).map((file) => <tr key={file.uploadFileId}><td><strong>{file.name}</strong></td><td><code>{file.reasonCode ?? "REJECTED"}</code></td><td title={`${file.sizeBytes} bytes`}>{formatBytes(file.sizeBytes)}</td></tr>)}</tbody></table></div></section> : null}</> : null}
        {files.length ? <><div className="import-selected-summary"><div><span className="status good"><i />已选择</span><h3>{files.length} 个文件 · {formatBytes(totalBytes)}</h3><p>文件相对路径会完整保留，上传前仍可重新选择。</p></div><div><button className="button secondary" type="button" onClick={() => setShowFiles((current) => !current)}>{showFiles ? "收起文件清单" : "查看文件清单"}</button><button className="button secondary" type="button" onClick={() => { setFiles([]); setShowFiles(false); }}>重新选择</button><button className="button" type="button" onClick={() => setStep(2)}>下一步</button></div></div>{showFiles ? <section className="panel import-file-preview"><div className="panel-head"><div><h2>已选择文件</h2><p>当前展示前 20 项，共 {files.length} 个文件。</p></div></div><div className="table-wrap"><table><thead><tr><th>相对路径</th><th>类型</th><th>大小</th></tr></thead><tbody>{files.slice(0, 20).map((file) => <tr key={`${file.path}-${file.size}`}><td><strong>{file.path}</strong></td><td>{file.name.toLocaleLowerCase().endsWith(".zip") ? "ZIP 压缩包" : "游戏文件"}</td><td title={`${file.size} bytes`}>{formatBytes(file.size)}</td></tr>)}</tbody></table></div></section> : null}</> : null}
      </section> : null}
      {step === 2 ? <section className="panel import-config-panel">
        <div className="panel-head"><div><h2>确认导入配置</h2><p>目标目录决定基础平台和推荐运行方式；配置会冻结到本次任务快照。</p></div><span className="status info"><i />步骤 2 / 3</span></div>
        <div className="panel-body">
          <div className="form-grid import-config-grid">
            <div className="field"><label htmlFor="directory">目标游戏目录</label><select id="directory" value={target} onChange={(event) => setTarget(event.target.value)}><option value="" disabled>{directories.length ? "请选择目标游戏目录" : "暂无可用游戏目录"}</option>{directories.map((directory) => <option value={directory.id} key={directory.id}>{directory.name}</option>)}</select><small>{reconfigureSource ? "可以保留原目录，也可以选择正确的平台目录后重新识别。" : "必须主动选择，避免将游戏导入到错误目录。"}</small></div>
            <div className="field"><label htmlFor="provider">元信息来源</label><select id="provider" value={provider} onChange={(event) => setProvider(event.target.value)}><option value="HASHEOUS">Hasheous 哈希查询</option><option value="NONE">不刮削</option></select></div>
            <div className="field"><label>游戏平台</label><input value={selectedDirectory?.platformName ?? "选择目录后显示"} disabled /></div>
            <div className="field"><label>推荐运行方式</label><input value={selectedDirectory?.coreName ?? "选择目录后显示"} disabled /></div>
          </div>
          {!reconfigureSource ? <fieldset className="field import-content-mode">
            <legend>内容布局</legend>
            <label><input type="radio" name="content-mode" value="STANDARD" checked={contentMode === "STANDARD"} onChange={() => setContentMode("STANDARD")} />普通游戏内容</label>
            <label><input type="radio" name="content-mode" value="MULTI_DISC_M3U_V1" checked={contentMode === "MULTI_DISC_M3U_V1"} onChange={() => setContentMode("MULTI_DISC_M3U_V1")} />多盘 M3U + CHD</label>
            {contentMode === "MULTI_DISC_M3U_V1" ? <small>{multiDiscSupported ? `此目录支持多盘导入，单组最多 ${selectedDirectory?.importCapabilities?.multiDisc?.maxDiscs ?? 8} 张光盘。` : "所选目录的运行方式不支持多盘导入，请更换目录。"}</small> : null}
          </fieldset> : null}
          {contentMode === "MULTI_DISC_M3U_V1" && preflight?.detected ? <div className={`feedback ${multiDiscInvalid ? "bad" : preflight.missingDiscCount ? "warn" : "good"}`} role="status"><strong>{preflight.missingDiscCount ? `将创建审核项并等待补齐 ${preflight.missingDiscCount} 张光盘` : "M3U 引用与所选文件一致"}</strong><p>完整 {preflight.completeGroupCount} 组、待补盘 {preflight.blockedGroupCount} 组、拒绝 {preflight.rejectedGroupCount} 组；未引用文件不会进入游戏内容。</p></div> : null}
          <div className="import-config-summary"><div><small>内容</small><strong>{fileCount} 个文件</strong></div><div><small>数据量</small><strong>{formatBytes(totalBytes)}</strong></div><div><small>目标</small><strong>{selectedDirectory?.name ?? "尚未选择"}</strong></div><div><small>布局</small><strong>{contentMode === "MULTI_DISC_M3U_V1" ? "多盘 M3U" : "普通内容"}</strong></div></div>
          <div className="import-stage-actions"><button className="button secondary" type="button" onClick={() => setStep(1)}>上一步</button><button className="button" type="button" disabled={busy || !target || multiDiscInvalid} onClick={() => void submitImport()}>{reconfigureSource ? "按新配置重新识别" : contentMode === "MULTI_DISC_M3U_V1" && preflight?.missingDiscCount ? "继续上传并在审核补齐" : "开始上传并验证"}</button></div>
        </div>
      </section> : null}
      {step === 3 ? <section className="panel import-progress-card"><StatusBadgeLike completed={Boolean(completedJobId)} busy={busy} /><h2>{completedJobId ? "导入任务已创建" : error ? reconfigureSource ? "重新配置没有完成" : "上传没有完成" : reconfigureSource ? "正在复用文件并重新识别" : "正在上传并验证"}</h2><p>{error || progress || (reconfigureSource ? "正在准备服务器中的既有文件…" : "正在准备安全上传会话…")}</p><div className="import-progress-track"><i style={{ width: `${uploadPercent}%` }} /></div><div className="import-progress-line"><span>{completedJobId ? "任务已进入后台处理" : progress || (reconfigureSource ? "准备复用文件…" : "准备上传…")}</span><strong>{uploadPercent}%</strong></div><div className="import-progress-steps"><div><strong>{reconfigureSource ? "复用服务器文件" : "上传文件"}</strong><span>{uploadPercent >= 76 ? "完成" : busy ? "处理中" : error ? "未完成" : "等待"}</span></div><div><strong>{reconfigureSource ? "应用新配置" : "完整性校验"}</strong><span>{uploadPercent >= 92 ? "完成" : uploadPercent >= 76 ? "处理中" : "等待"}</span></div><div><strong>创建导入任务</strong><span>{completedJobId ? "完成" : uploadPercent >= 92 ? "处理中" : "等待"}</span></div></div><div className="import-stage-actions"><span>{completedJobId ? "后台会继续识别、运行检查和游戏信息准备。" : reconfigureSource ? "文件内容不会再次通过网络上传。" : "上传过程中可以离开页面；已创建的后台任务会继续运行。"}</span>{completedJobId ? <button className="button" type="button" onClick={() => { router.push("/admin/imports/tasks"); router.refresh(); }}>查看任务进度 →</button> : error ? <button className="button secondary" type="button" onClick={() => setStep(2)}>返回配置</button> : null}</div></section> : null}
    </div>
  );
}

function StatusBadgeLike({ completed, busy }: { completed: boolean; busy: boolean }) {
  return <span className={`status ${completed ? "good" : busy ? "info" : "bad"}`}><i />{completed ? "已完成" : busy ? "正在处理" : "需要处理"}</span>;
}
