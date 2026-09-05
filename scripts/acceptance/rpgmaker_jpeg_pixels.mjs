import {createRequire} from "node:module";
import {pathToFileURL} from "node:url";

// Reuse the image decoder already installed and pinned by Next, not a second
// image dependency. The original upload bytes remain the evidence and hash.
const sharp = createRequire(new URL("../../web/package.json", import.meta.url))("sharp");
const invalid = () => {throw new Error("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_IMAGE_INVALID");};

export async function jpegPixelsAsPng(bytes) {
  if (!Buffer.isBuffer(bytes) || bytes.length < 4 || bytes.length > 16 * 1024 * 1024 ||
      bytes[0] !== 0xff || bytes[1] !== 0xd8 || bytes[2] !== 0xff) {invalid();}
  try {
    const decoder = sharp(bytes, {limitInputPixels: 4096 * 4096, failOn: "warning"});
    const metadata = await decoder.metadata();
    if (metadata.format !== "jpeg" || metadata.width < 320 || metadata.width > 4096 ||
        metadata.height < 180 || metadata.height > 4096) {invalid();}
    return await decoder.png({palette: false, progressive: false}).toBuffer();
  } catch {invalid();}
}

async function main() {
  const parts = [];
  let size = 0;
  for await (const part of process.stdin) {
    size += part.length;
    if (size > 16 * 1024 * 1024) {invalid();}
    parts.push(part);
  }
  process.stdout.write(await jpegPixelsAsPng(Buffer.concat(parts)));
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch(() => {process.stderr.write("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_IMAGE_INVALID\n"); process.exitCode = 1;});
}
