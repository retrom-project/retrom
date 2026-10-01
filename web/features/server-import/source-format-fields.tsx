import type { SourceFormat } from "./source-import-model";

export function SourceFormatFields({ format, extensionFilter, busy, onFormat, onExtensionFilter }: {
  format: SourceFormat | "";
  extensionFilter: string;
  busy: boolean;
  onFormat: (value: SourceFormat | "") => void;
  onExtensionFilter: (value: string) => void;
}) {
  function select(value: string) {
    if (value === "BASIC" || value === "PEGASUS" || value === "GAMELIST") {onFormat(value);}
  }
  return <div className="source-format-fields">
    <label>文件组织格式
      <select className="select" aria-label="文件组织格式" value={format} disabled={busy} onChange={(event) => select(event.target.value)}>
        <option value="" disabled>请选择文件组织格式</option>
        <option value="BASIC">basic</option>
        <option value="PEGASUS">metadata.pegasus.txt</option>
        <option value="GAMELIST">gamelist.xml</option>
      </select>
    </label>
    <label>扩展名筛选
      <input className="input" aria-label="扩展名筛选" title="仅 basic 可用；以 . 开头，多个用 ; 分隔，留空允许全部文件。" value={extensionFilter}
        placeholder="如 .nes;.zip；留空允许全部扩展名" maxLength={2048} disabled={busy || format !== "BASIC"}
        onChange={(event) => onExtensionFilter(event.target.value)} />
    </label>
  </div>;
}
