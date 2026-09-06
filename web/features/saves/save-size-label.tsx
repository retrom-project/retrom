import { formatSaveSize } from "./save-library";

export function SaveSizeLabel({ sizeBytes }: { sizeBytes: number }) {
  const size = formatSaveSize(sizeBytes);
  return <span className="save-library-size" aria-label={`存档大小 ${size}`} title={`${sizeBytes.toLocaleString("zh-CN")} bytes`}>{size}</span>;
}
