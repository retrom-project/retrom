import type { Schema } from "@/lib/api/types";

export function LibraryFilterFields({
  directory,
  tag,
  sort,
  directories,
  tags,
  onDirectory,
  onTag,
  onSort,
}: {
  directory: string;
  tag: string;
  sort: "title" | "recent";
  directories: Schema<"Directory">[];
  tags: Schema<"Tag">[];
  onDirectory: (value: string) => void;
  onTag: (value: string) => void;
  onSort: (value: "title" | "recent") => void;
}) {
  return (
    <>
      <label className="library-filter-field">
        <span>游戏目录</span>
        <select
          aria-label="游戏目录"
          value={directory}
          onChange={(event) => onDirectory(event.target.value)}
        >
          <option value="">全部目录</option>
          {directories.map((item) => (
            <option key={item.id} value={item.id}>
              {item.name}
            </option>
          ))}
        </select>
      </label>
      <label className="library-filter-field">
        <span>标签</span>
        <select
          aria-label="标签"
          value={tag}
          onChange={(event) => onTag(event.target.value)}
        >
          <option value="">全部标签</option>
          {tags.map((item) => (
            <option key={item.id} value={item.id}>
              {item.name}
            </option>
          ))}
        </select>
      </label>
      <label className="library-filter-field">
        <span>排序</span>
        <select
          aria-label="排序"
          value={sort}
          onChange={(event) => {
            if (
              event.target.value === "title" ||
              event.target.value === "recent"
            ) {
              onSort(event.target.value);
            }
          }}
        >
          <option value="title">游戏标题</option>
          <option value="recent">最近游玩</option>
        </select>
      </label>
    </>
  );
}
