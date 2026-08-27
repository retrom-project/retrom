export async function readBoundedResponse(response: Response, maximumBytes: number, errorCode = "PLAYER_SAVE_STATE_TOO_LARGE") {
  const declared = Number(response.headers.get("content-length") ?? "0");
  if (Number.isFinite(declared) && declared > maximumBytes) {throw new Error(errorCode);}
  if (!response.body) {
    const bytes = new Uint8Array(await response.arrayBuffer());
    if (bytes.byteLength > maximumBytes) {throw new Error(errorCode);}
    return bytes;
  }
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) {break;}
      length += value.byteLength;
      if (length > maximumBytes) {throw new Error(errorCode);}
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  const result = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) {result.set(chunk, offset); offset += chunk.byteLength;}
  return result;
}

export function reportsNativeExit(mode: "single" | "netplay", finishing = false) {
  return mode === "single" && !finishing;
}

export function formatPlayerBytes(bytes: number) {
  if (bytes < 1024 * 1024) {return `${(bytes / 1024).toFixed(1)} KiB`;}
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}
