"use client";

import type {RPGMakerReview} from "./review-actions-model";
import {RPGPackControls} from "./review-rpg-packs";

export function RPGDependenciesCard({value, disabled, onChange}: {
  value: RPGMakerReview; disabled: boolean; onChange: (next: RPGMakerReview) => void;
}) {
  const packLabel = value.selfContained || value.selfContainedOverride
    ? "项目自包含" : `${value.runtimePackSelections.length} 个已锁定运行包`;
  return <section className="panel review-rpg-dependencies">
    <div className="panel-head"><div><h2>RPG Maker 运行依赖</h2>
      <p>检查引擎与资源包；可按需运行游戏、保存截图和试玩存档，然后返回本页审核。</p>
    </div></div>
    <div className="panel-body">
      <div className="review-rpg-facts">
        <div><span>所选版本</span><strong>{generationLabel(value.generation)}</strong></div>
        <div><span>内容校验</span><strong>{value.evidenceConfidence === "MATCHED" ? "版本精确匹配" : "2000/2003 家族匹配"}</strong></div>
        <div><span>运行包</span><strong>{packLabel}</strong></div>
      </div>
      <RPGPackControls value={value} disabled={disabled} onChange={onChange} />
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
