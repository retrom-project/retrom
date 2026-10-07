import { AppIcon } from "@/components/app-icon";
import { SaveGameFilter } from "./save-game-filter";
export type SaveFiltersValue = {
  q: string;
  gameId: string;
  kind: "" | "checkpoint" | "game_save";
  sort: "recent" | "title";
};
export function SaveFilters({
  value,
  total,
  onChange,
}: {
  value: SaveFiltersValue;
  total: number | undefined;
  onChange: (value: SaveFiltersValue) => void;
}) {
  return (
    <section className="save-library-toolbar" aria-label="筛选存档">
      <label className="save-library-search">
        <span>搜索</span>
        <span>
          <AppIcon name="search" />
          <input
            type="search"
            value={value.q}
            aria-label="搜索存档"
            placeholder="搜索游戏或存档名称"
            onChange={(event) => onChange({ ...value, q: event.target.value })}
          />
        </span>
      </label>
      <SaveGameFilter
        gameId={value.gameId}
        onChange={(gameId) => onChange({ ...value, gameId })}
      />
      <label>
        <span>存档类型</span>
        <select
          value={value.kind}
          onChange={(event) =>
            onChange({ ...value, kind: saveKind(event.target.value) })
          }
        >
          <option value="">全部类型</option>
          <option value="checkpoint">即时存档</option>
          <option value="game_save">游戏内存档</option>
        </select>
      </label>
      <label>
        <span>排列</span>
        <select
          value={value.sort}
          onChange={(event) =>
            onChange({
              ...value,
              sort: event.target.value === "title" ? "title" : "recent",
            })
          }
        >
          <option value="recent">最近保存优先</option>
          <option value="title">游戏名称</option>
        </select>
      </label>
      <p>
        当前共 <strong>{total ?? "—"}</strong> 份
      </p>
    </section>
  );
}
function saveKind(value: string): SaveFiltersValue["kind"] {
  return value === "game_save" || value === "checkpoint" ? value : "";
}
