import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { readFile, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const demoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

export async function readManifest() {
  const contents = await readFile(path.join(demoRoot, "assets-manifest.json"), "utf8");
  const manifest = JSON.parse(contents);
  if (manifest.schemaVersion !== 2 || !Array.isArray(manifest.assets) || !manifest.packages) {
    throw new Error("Unsupported asset manifest");
  }
  return manifest;
}

export async function sha256(filePath) {
  const digest = createHash("sha256");
  await new Promise((resolve, reject) => {
    const stream = createReadStream(filePath);
    stream.on("data", (chunk) => digest.update(chunk));
    stream.on("error", reject);
    stream.on("end", resolve);
  });
  return digest.digest("hex");
}

export async function verifyFile(filePath, asset) {
  const info = await stat(filePath);
  if (!info.isFile() || info.size !== asset.size) {
    throw new Error(`${asset.target}: expected ${asset.size} bytes, received ${info.size}`);
  }
  const digest = await sha256(filePath);
  if (digest !== asset.sha256) {
    throw new Error(`${asset.target}: SHA-256 mismatch (${digest})`);
  }
}

export function parseOption(argv, name) {
  const index = argv.indexOf(name);
  if (index === -1) return null;
  const value = argv[index + 1];
  if (!value || value.startsWith("--")) throw new Error(`${name} requires a path`);
  return path.resolve(value);
}
