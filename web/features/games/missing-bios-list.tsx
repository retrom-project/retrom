import type { Schema } from "@/lib/api/types";

export function MissingBiosList({ items }: { items: Schema<"ReviewMissingBIOS">[] }) {
  return <ul className="review-bios-requirements">
    {items.map((item) => <li key={item.key}>
      <strong>{item.name}</strong>
      <span>核心：{item.coreId} · 必需</span>
    </li>)}
  </ul>;
}
