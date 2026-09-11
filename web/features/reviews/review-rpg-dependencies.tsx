"use client";

import type {RPGMakerReview} from "./review-actions-model";

export function RPGDependenciesCard({value, disabled, onChange}: {
  value: RPGMakerReview; disabled: boolean; onChange: (next: RPGMakerReview) => void;
}) {
  const needsResources = value.externalRTPRequirements.length > 0;
  const canConfirm = !["RPGMV", "RPGMZ"].includes(value.generation);
  const resourceLabel = value.selfContainedOverride ? "已人工确认自包含" : needsResources ? "声明了外部 RTP" : "未声明外部 RTP";
  return <section className="panel review-rpg-dependencies">
    <div className="panel-head"><div><h2>RPG Maker 项目检查</h2>
      <p>游戏包需包含运行所需的素材；系统不提供或挂载外部 RTP。</p>
    </div></div>
    <div className="panel-body">
      <div className="review-rpg-facts">
        <div><span>所选版本</span><strong>{generationLabel(value.generation)}</strong></div>
        <div><span>内容校验</span><strong>{value.evidenceConfidence === "MATCHED" ? "版本精确匹配" : "2000/2003 家族匹配"}</strong></div>
        <div><span>项目资源</span><strong>{resourceLabel}</strong></div>
      </div>
      {needsResources ? <p className="review-rpg-resource-notice">检测到外部 RTP：{value.externalRTPRequirements.map((entry) => entry.declaredName).join("、")}。请补齐游戏素材后重新导入，或由管理员确认自包含并放行。</p> : null}
      {canConfirm ? <label className="review-rpg-self-contained">
        <input type="checkbox" aria-label="确认项目自包含 RTP" checked={value.selfContainedOverride} disabled={disabled} onChange={(event) => onChange({...value, selfContainedOverride: event.target.checked})} />
        <span><strong>确认项目自包含 RTP</strong><small>允许忽略外部 RTP 声明并通过审核；不会补充缺失素材，游戏仍可能无法正常运行。</small></span>
      </label> : null}
    </div>
  </section>;
}

function generationLabel(generation: string) {
  const labels: Record<string, string> = {
    RPG2000: "RPG Maker 2000", RPG2003: "RPG Maker 2003", RPGXP: "RPG Maker XP",
    RPGVX: "RPG Maker VX", RPGVXACE: "RPG Maker VX Ace", RPGMV: "RPG Maker MV", RPGMZ: "RPG Maker MZ",
  };
  return labels[generation] ?? generation;
}
