"use client";
import Link from "next/link";
import { useState } from "react";
import type { Save } from "@/lib/api/types";
import { AppIcon } from "@/components/app-icon";
import { BrowserTime } from "@/components/browser-time";
import { SaveCard } from "./save-card";
export function SaveGroups({
  saves,
  onChange,
}: {
  saves: Save[];
  onChange: () => void;
}) {
  const groups = new Map<string, Save[]>();
  for (const save of saves) {
    const group = groups.get(save.game.id) ?? [];
    group.push(save);
    groups.set(save.game.id, group);
  }
  return (
    <div className="save-library-groups">
      {[...groups].map(([gameId, items]) => (
        <SaveGroup key={gameId} items={items} onChange={onChange} />
      ))}
    </div>
  );
}
function SaveGroup({ items, onChange }: { items: Save[]; onChange: () => void }) {
  const [expanded, setExpanded] = useState(false);
  const latest = Math.max(...items.map((save) => save.createdAtMs));
  return (
        <section className={`save-library-group${expanded ? " is-expanded" : ""}${items.length === 5 ? " has-five-saves" : ""}`}>
          <header className="save-library-group-head">
            <div className="save-library-group-main">
              <span className="save-library-group-icon">
                <AppIcon name="gamepad" />
              </span>
              <div>
                <h2>{items[0].game.title}</h2>
                <p>{items[0].game.directoryName}</p>
              </div>
            </div>
            <div className="save-library-group-meta">
              <span>
                <strong>{items.length}</strong> 份存档
              </span>
              <span>最近保存 <strong><BrowserTime value={latest} /></strong></span>
              <Link href={`/games/${items[0].game.id}`}>查看游戏详情</Link>
            </div>
          </header>
          <div className="save-library-grid">
            {items.map((save) => (
              <SaveCard key={save.id} save={save} onChange={onChange} />
            ))}
            {items.length === 1 ? <div className="save-library-empty-slot">当前结果中只有这一份存档</div> : null}
          </div>
          {items.length > 4 ? <div className="save-library-group-foot"><button type="button" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>{expanded ? "收起存档 ↑" : `展开全部 ${items.length} 份 ↓`}</button></div> : null}
        </section>
  );
}
