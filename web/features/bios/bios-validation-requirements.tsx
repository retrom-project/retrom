import type { Schema } from "@/lib/api/types";

export function BiosValidationRequirements({ items, catalog }: {
  items: Schema<"BiosValidationRequirement">[];
  catalog: Schema<"RuntimeCatalog"> | null;
}) {
  return <details className="runtime-validation">
    <summary>文件校验要求</summary>
    <div className="runtime-validation-list">
      {items.map((item) => {
        const core = catalog?.cores.find((core) => core.id === item.coreId);
        const known = item.sizeBytes !== null || Boolean(item.sha256 || item.md5);
        return <section key={item.coreId} aria-label={`核心 ${item.coreId} 的文件校验要求`}>
          <h4>核心：{core ? `${core.name}（${item.coreId}）` : item.coreId}</h4>
          {known ? <dl className="runtime-technical">
            {item.sizeBytes !== null ? <><dt>要求大小</dt><dd>{item.sizeBytes.toLocaleString("zh-CN")} 字节</dd></> : null}
            {item.sha256 ? <><dt>SHA-256</dt><dd>{item.sha256}</dd></> : null}
            {item.md5 ? <><dt>MD5</dt><dd>{item.md5}</dd></> : null}
          </dl> : <p>未声明固定大小或校验值。</p>}
        </section>;
      })}
    </div>
  </details>;
}
