import type { Schema } from "@/lib/api/types";
import { formatBytes } from "@/lib/format-bytes";

export function MissingRomMembers({ items }: { items: Schema<"ReviewMissingROMMember">[] }) {
  if (!items.length) { return null; }
  return <details className="review-required-members" open={items.length <= 5}>
    <summary>DAT 必需文件不完整（{items.length} 项）</summary>
    <ul className="review-bios-requirements">
      {items.map((item, index) => <li key={`${item.coreId}:${item.romSet}:${item.member}:${index}`}>
        <strong>{item.member}</strong>
        <span>{item.reason === "SIZE_MISMATCH" ? "大小不符" : "缺少文件"} · 要求 {formatBytes(item.sizeBytes)}</span>
        <span>核心：{item.coreId} · ROM 集：{item.romSet} · {item.role === "PARENT" ? "Parent" : item.role}</span>
      </li>)}
    </ul>
  </details>;
}
