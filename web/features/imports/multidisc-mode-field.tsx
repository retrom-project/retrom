import { formatBytes } from "@/lib/backend";

export function MultiDiscModeField({
  selected,
  detectedGroupCount,
  maxDiscs,
  maxTotalBytes,
  onChange,
}: {
  selected: boolean;
  detectedGroupCount: number;
  maxDiscs: number;
  maxTotalBytes: number;
  onChange: (selected: boolean) => void;
}) {
  const helpID = "multi-disc-mode-help";
  return <fieldset className="field import-content-mode multi-disc-mode-field">
    <legend>内容布局</legend>
    <label className="multi-disc-mode-choice">
      <input
        type="checkbox"
        checked={selected}
        aria-describedby={helpID}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span><strong>多盘游戏（M3U + CHD）</strong><small>{detectedGroupCount ? `已自动识别 ${detectedGroupCount} 个多盘游戏` : "手动选择多盘目录模式"}</small></span>
    </label>
    <p id={helpID}>每个 M3U 所在目录对应一个游戏。M3U 决定光盘顺序，未被引用的文件将忽略。</p>
    <small>每个游戏 2–{maxDiscs} 张 CHD，总计不超过 {formatBytes(maxTotalBytes)}。</small>
    <span className="sr-only" aria-live="polite">{selected ? "已选择多盘游戏模式" : "已选择普通游戏内容模式"}</span>
  </fieldset>;
}
