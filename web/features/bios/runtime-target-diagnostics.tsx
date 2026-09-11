import { StatusBadge } from "@/components/ui";
import type { components } from "@/lib/api/generated/schema";

export type RuntimeTargetList = components["schemas"]["RuntimeTargetList"];

const rpgTargetOrder = [
  "rpgmaker-2000", "rpgmaker-2003", "rpgmaker-xp", "rpgmaker-vx",
  "rpgmaker-vx-ace", "rpgmaker-mv", "rpgmaker-mz",
];

export function RuntimeTargetDiagnostics({catalog}: {catalog: RuntimeTargetList}) {
  const targets = new Map(catalog.items.map((item) => [item.targetId, item]));
  return <section className="runtime-core-diagnostics" aria-labelledby="runtime-core-diagnostics-title">
    <header><div><span>管理员诊断</span><h2 id="runtime-core-diagnostics-title">Runtime Provider / Target</h2></div><p>这里展示 Product Core 当前绑定的逻辑 Runtime Target。</p></header>
    <div className="runtime-core-diagnostic-grid">{rpgTargetOrder.map((targetId) => {
      const target = targets.get(targetId);
      return <article key={targetId}><div><strong>{target?.displayName ?? targetId}</strong><small>{target?.coreName ?? "RPG Maker"}</small></div>
        {target ? <><code>{target.providerId}/{target.targetId}</code><small>Provider v{target.providerVersion}</small><StatusBadge tone={target.launchPolicy === "SUPPORTED" ? "good" : target.launchPolicy === "EXPERIMENTAL" ? "warn" : "bad"}>{target.launchPolicy === "SUPPORTED" ? "可启动" : target.launchPolicy === "EXPERIMENTAL" ? "实验性" : "已禁用"}</StatusBadge></> : <StatusBadge tone="bad">未登记</StatusBadge>}
      </article>;
    })}</div>
  </section>;
}
