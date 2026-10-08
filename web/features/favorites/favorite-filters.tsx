import { useHorizontalWheel } from "@/lib/use-horizontal-wheel";
import { AppIcon } from "@/components/app-icon";
import type { Directory } from "@/lib/api/types";
export function FavoriteFilters({
  query,
  sort,
  selecting,
  directory,
  directories,
  onQuery,
  onSort,
  onDirectory,
  onToggle,
}: {
  query: string;
  sort: "title" | "recent";
  selecting: boolean;
  directory: string;
  directories: Directory[];
  onQuery: (value: string) => void;
  onSort: (value: "title" | "recent") => void;
  onDirectory: (value: string) => void;
  onToggle: () => void;
}) {
  const optionsRail = useHorizontalWheel<HTMLDivElement>();
  const platformRail = useHorizontalWheel<HTMLDivElement>();
  return (
    <>
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
      <div ref={platformRail} className="favorite-platforms">
        <span>游戏目录</span>
        <div ref={optionsRail} className="favorite-platform-options" role="group" aria-label="筛选游戏目录">
        <button
          className={!directory ? "is-active" : ""}
          onClick={() => onDirectory("")}
        >
          全部
        </button>
        {directories
          .filter((item) => item.gameCount > 0)
          .map((item) => (
            <button
              key={item.id}
              className={directory === item.id ? "is-active" : ""}
              onClick={() => onDirectory(item.id)}
            >
              {item.name}
            </button>
          ))}
        </div>
      </div>
    </>
  );
}
