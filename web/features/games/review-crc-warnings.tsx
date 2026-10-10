import type {Schema} from "@/lib/api/types";

export function ReviewCRCWarnings({warnings}: {warnings: Schema<"ReviewReadiness">["crcWarnings"]}) {
  if (!warnings.length) {return null;}
  const labels = {ROM: "游戏 ROM", PARENT: "Parent ROM", BIOS: "BIOS"};
  return <section className="review-crc-warnings" aria-label="CRC 检查提示">
    <h3>文件 CRC 与核心目录不一致</h3>
    <p>可能是汉化或修改版。一键快速审核会跳过此游戏，请查看后手动通过。可以先运行测试，测试不是通过审核的必需条件。</p>
    <details><summary>查看 {warnings.length} 项 CRC 提示</summary>
      <ul>{warnings.map(warning => <li key={`${warning.file}/${warning.member}/${warning.expectedCRC32}`}>
        <strong>{labels[warning.role]}：{warning.member}</strong>
        <span>所在文件：{warning.file}</span>
        <span>目录 CRC：<code>{warning.expectedCRC32}</code> · 文件 CRC：<code>{warning.actualCRC32}</code></span>
      </li>)}</ul>
    </details>
  </section>;
}
