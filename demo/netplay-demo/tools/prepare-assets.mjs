import { createHash } from "node:crypto";
import { copyFile, mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { gunzipSync } from "node:zlib";
import { demoRoot, parseOption, readManifest, verifyFile } from "./asset-lib.mjs";

const pegasusRoot = parseOption(process.argv.slice(2), "--pegasus-root")
  ?? (process.env.RETROM_PEGASUS_ROOT ? path.resolve(process.env.RETROM_PEGASUS_ROOT) : null);

if (!pegasusRoot) throw new Error("Set RETROM_PEGASUS_ROOT or pass --pegasus-root");

function verifyArchive(bytes, integrity, label) {
  const [algorithm, expected] = integrity.split("-", 2);
  if (algorithm !== "sha512" || !expected) throw new Error(`${label}: unsupported package integrity`);
  const actual = createHash("sha512").update(bytes).digest("base64");
  if (actual !== expected) throw new Error(`${label}: package integrity mismatch`);
}

function readTarFiles(archive, requested, label) {
  const tar = gunzipSync(archive, { maxOutputLength: 128 * 1024 * 1024 });
  const found = new Map();
  for (let offset = 0; offset + 512 <= tar.length;) {
    const header = tar.subarray(offset, offset + 512);
    if (header.every((byte) => byte === 0)) break;
    const text = (start, end) => header.subarray(start, end).toString("utf8").replace(/\0.*$/s, "");
    const name = [text(345, 500), text(0, 100)].filter(Boolean).join("/");
    const sizeText = text(124, 136).trim();
    const size = Number.parseInt(sizeText || "0", 8);
    if (!Number.isSafeInteger(size) || size < 0) throw new Error(`${label}: invalid tar member size`);
    const dataOffset = offset + 512;
    const nextOffset = dataOffset + Math.ceil(size / 512) * 512;
    if (nextOffset > tar.length) throw new Error(`${label}: truncated tar archive`);
    const relative = name.startsWith("package/") ? name.slice("package/".length) : null;
    if (relative && requested.has(relative)) found.set(relative, Buffer.from(tar.subarray(dataOffset, dataOffset + size)));
    offset = nextOffset;
  }
  for (const source of requested) {
    if (!found.has(source)) throw new Error(`${label}: package is missing ${source}`);
  }
  return found;
}

async function packageFiles(manifest) {
  const result = new Map();
  const cacheRoot = path.join(demoRoot, ".cache", "packages");
  await mkdir(cacheRoot, { recursive: true });
  for (const [packageName, descriptor] of Object.entries(manifest.packages)) {
    const cacheFile = path.join(cacheRoot, `${packageName.replaceAll("/", "-").replaceAll("@", "")}-${descriptor.version}.tgz`);
    let archive;
    try {
      archive = await readFile(cacheFile);
      verifyArchive(archive, descriptor.integrity, packageName);
    } catch {
      const response = await fetch(descriptor.tarball, {
        redirect: "error",
        signal: AbortSignal.timeout(60000)
      });
      if (!response.ok) throw new Error(`${packageName}: package download failed with ${response.status}`);
      archive = Buffer.from(await response.arrayBuffer());
      verifyArchive(archive, descriptor.integrity, packageName);
      await writeFile(cacheFile, archive);
    }
    const requested = new Set(manifest.assets
      .filter((asset) => asset.package === packageName)
      .map((asset) => asset.source));
    result.set(packageName, readTarFiles(archive, requested, packageName));
  }
  return result;
}

const manifest = await readManifest();
const packages = await packageFiles(manifest);
for (const asset of manifest.assets) {
  const target = path.join(demoRoot, asset.target);
  await mkdir(path.dirname(target), { recursive: true });
  if (asset.kind === "license") {
    const response = await fetch(asset.source, {
      redirect: "error",
      signal: AbortSignal.timeout(60000)
    });
    if (!response.ok) throw new Error(`${asset.target}: license download failed with ${response.status}`);
    const bytes = Buffer.from(await response.arrayBuffer());
    const digest = createHash("sha256").update(bytes).digest("hex");
    if (bytes.length !== asset.size || digest !== asset.sha256) {
      throw new Error(`${asset.target}: downloaded license did not match the manifest`);
    }
    await writeFile(target, bytes);
  } else if (asset.kind === "rom") {
    const source = path.join(pegasusRoot, asset.source);
    await verifyFile(source, asset);
    await copyFile(source, target);
  } else {
    const bytes = packages.get(asset.package)?.get(asset.source);
    if (!bytes) throw new Error(`${asset.target}: package source is unavailable`);
    await writeFile(target, bytes);
  }
  await verifyFile(target, asset);
}

console.log(`Prepared and verified ${manifest.assets.length} standalone demo assets.`);
