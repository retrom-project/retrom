import Link from "next/link";
import type { Save } from "@/lib/api/types";
import { AppIcon } from "@/components/app-icon";
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
        <section className="save-library-group is-expanded" key={gameId}>
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
              <Link href={`/games/${gameId}`}>查看游戏详情</Link>
            </div>
          </header>
          <div className="save-library-grid">
            {items.map((save) => (
              <SaveCard key={save.id} save={save} onChange={onChange} />
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}
