import type { Schema } from "@/lib/api/types";

export function BiosValidationRequirements({ items, catalog, verification }: {
  items: Schema<"BiosValidationRequirement">[];
  verification?: Schema<"BiosVerification">;
  catalog: Schema<"RuntimeCatalog"> | null;
}) {
  return <details className="runtime-validation">
    <summary>文件校验要求</summary>
    <div className="runtime-validation-list">
      {verification ? <section aria-label="已安装文件的成员校验结果">
        <h4>DAT 成员校验</h4>
        <p>DAT 成员匹配：{verification.matchedMembers} / {verification.expectedMembers}</p>
        {verification.status === "invalid" ? <p>无法读取或校验包内文件，请替换完整的 ZIP 文件。</p> : null}
        {verification.missingMembers.map((name) => <p key={name}>缺少或不匹配：{name}</p>)}
      </section> : null}
      {items.map((item) => {
        const core = catalog?.cores.find((core) => core.id === item.coreId);
        const known = item.sizeBytes !== null || Boolean(item.sha256 || item.md5);
        return <section key={item.coreId} aria-label={`核心 ${item.coreId} 的文件校验要求`}>
          <h4>核心：{core ? `${core.name}（${item.coreId}）` : item.coreId}</h4>
          {known ? <dl className="runtime-technical">
            {item.sizeBytes !== null ? <><dt>要求大小</dt><dd>{item.sizeBytes.toLocaleString("zh-CN")} 字节</dd></> : null}
            {item.sha256 ? <><dt>SHA-256</dt><dd>{item.sha256}</dd></> : null}
            {item.md5 ? <><dt>MD5</dt><dd>{item.md5}</dd></> : null}
          </dl> : <p>{verification ? "按 DAT 校验包内成员内容。" : "未声明固定大小或校验值。"}</p>}
        </section>;
      })}
    </div>
  </details>;
}
