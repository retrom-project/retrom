import { AppIcon } from "@/components/app-icon";
export function FavoriteFilters({
  query,
  sort,
  selecting,
  onQuery,
  onSort,
  onToggle,
}: {
  query: string;
  sort: "title" | "recent";
  selecting: boolean;
  onQuery: (value: string) => void;
  onSort: (value: "title" | "recent") => void;
  onToggle: () => void;
}) {
  return (
    <div className="favorite-toolbar">
      <label>
        搜索收藏
        <span className="favorite-search">
          <AppIcon name="search" />
          <input
            aria-label="搜索收藏"
            placeholder="输入游戏标题"
            value={query}
            onChange={(event) => onQuery(event.target.value)}
          />
        </span>
      </label>
      <label>
        排序方式
        <select
          value={sort}
          onChange={(event) =>
            onSort(event.target.value === "title" ? "title" : "recent")
          }
        >
          <option value="recent">最近游玩</option>
          <option value="title">游戏名称</option>
        </select>
      </label>
      <button className="button secondary" onClick={onToggle}>
        {selecting ? "结束整理" : "批量整理"}
      </button>
    </div>
  );
}
