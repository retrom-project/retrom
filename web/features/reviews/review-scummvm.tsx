export type ScummVMReview = {
  kind: "SCUMMVM";
  selectedCandidateId: string;
  detection: { candidates: Array<{
    id: string; root: string; engineId: string; gameId: string; description: string;
    language: string; platform: string; extra: string; blocker: string;
  }> };
};

export function ScummVMSelection({value, disabled, onChange}: {
  value: ScummVMReview; disabled: boolean; onChange: (candidateId: string) => void;
}) {
  return <section className="panel" aria-labelledby="scummvm-selection-title">
    <div className="panel-head"><div><h2 id="scummvm-selection-title">ScummVM 游戏识别</h2>
      <p>确认游戏目录、语言和版本后即可运行。多个识别结果需要在审核中选择。</p></div></div>
    <div className="panel-body">{value.detection.candidates.length ? <label className="field">运行版本
      <select value={value.selectedCandidateId} disabled={disabled} onChange={(event) => onChange(event.target.value)}>
        <option value="" disabled>请选择要运行的游戏版本</option>
        {value.detection.candidates.map((candidate) => <option key={candidate.id} value={candidate.id} disabled={Boolean(candidate.blocker)}>
          {[candidate.description, candidate.root || "根目录", candidate.language, candidate.platform, candidate.extra, blockerLabel(candidate.blocker)].filter(Boolean).join(" · ")}
        </option>)}
      </select>
    </label> : <p className="feedback warn">没有识别到可运行的游戏，请检查是否包含完整游戏数据。</p>}</div>
  </section>;
}

function blockerLabel(blocker: string) {
  if (blocker === "UNKNOWN_VARIANT") {return "未知版本，暂不可运行";}
  if (blocker === "ENGINE_UNAVAILABLE") {return "当前未提供此引擎";}
  return blocker ? "此游戏暂不受支持" : "";
}

export function reviewScummVM(validation: {dependencySnapshot?: ScummVMReview} | null, candidateId?: string) {
  const snapshot = validation?.dependencySnapshot;
  return snapshot?.kind === "SCUMMVM" ? {...snapshot, selectedCandidateId: candidateId ?? snapshot.selectedCandidateId} : null;
}
