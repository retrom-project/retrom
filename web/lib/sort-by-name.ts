import { pinyin } from "pinyin-pro";

const alphabetic = new Intl.Collator("en", { sensitivity: "base", numeric: true });

/** Put Chinese names in the same alphabet as Latin names, without changing labels. */
export function sortByName<T extends { id: string; name: string }>(items: readonly T[]): T[] {
  return items.map(item => ({ item, key: pinyin(item.name.normalize("NFKC").trim(), {
    toneType: "none", type: "array", nonZh: "consecutive",
  }).join("") }))
    .sort((a, b) => alphabetic.compare(a.key, b.key) || alphabetic.compare(a.item.name, b.item.name)
      || a.item.id.localeCompare(b.item.id, "en"))
    .map(({ item }) => item);
}
