import { useHorizontalWheel } from "@/lib/use-horizontal-wheel";
import type { Directory, Schema } from "@/lib/api/types";

export function LibraryPlatformFilter({
  platforms,
  directories,
  selected,
  onSelect,
}: {
  platforms: Schema<"Platform">[];
  directories: Directory[];
  selected: string;
  onSelect: (value: string) => void;
}) {
  const platformRail = useHorizontalWheel<HTMLDivElement>();
  const present = new Set(directories.map((directory) => directory.platformId));
  const choices = platforms.filter((platform) => present.has(platform.id));
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
          onClick={() => onSelect(platform.id)}
        >
          {platform.name}
        </button>
      ))}
    </div>
  );
}
