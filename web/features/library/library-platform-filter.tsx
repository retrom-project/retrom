import { useHorizontalWheel } from "@/lib/use-horizontal-wheel";
import type { Directory, Schema } from "@/lib/api/types";
import { sortByName } from "@/lib/sort-by-name";

export function LibraryPlatformFilter({
  platforms,
  directories,
  selected,
  onSelect,
  showCounts = true,
}: {
  platforms: Schema<"Platform">[];
  directories: Directory[];
  selected: string;
  onSelect: (value: string) => void;
  showCounts?: boolean;
}) {
  const platformRail = useHorizontalWheel<HTMLDivElement>();
  const counts = new Map<string, number>();
  for (const directory of directories) {
    if (directory.enabled) {
      counts.set(directory.platformId, (counts.get(directory.platformId) ?? 0) + directory.gameCount);
    }
  }
  const choices = sortByName(platforms.filter((platform) => (counts.get(platform.id) ?? 0) > 0));
  return (
    <div ref={platformRail} className="library-platform-row" aria-label="游戏平台筛选">
      <span className="library-platform-label">平台</span>
      <button
        className={!selected ? "is-active" : ""}
        aria-pressed={!selected}
        onClick={() => onSelect("")}
      >
        全部平台
      </button>
      {choices.map((platform) => (
        <button
          key={platform.id}
          className={selected === platform.id ? "is-active" : ""}
          aria-pressed={selected === platform.id}
          title={showCounts ? `${platform.name}：${counts.get(platform.id)} 款已发布游戏` : platform.name}
          onClick={() => onSelect(platform.id)}
        >
          {platform.name}{showCounts ? `(${formatPlatformCount(counts.get(platform.id) ?? 0)})` : ""}
        </button>
      ))}
    </div>
  );
}

function formatPlatformCount(count: number) { return count > 1000 ? "999+" : String(count); }
