import { copyFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { demoRoot, parseOption, readManifest, verifyFile } from "./asset-lib.mjs";

const runtimeRoot = parseOption(process.argv.slice(2), "--runtime-root")
  ?? path.resolve(demoRoot, "../..", "data/runtime/emulatorjs/4.2.3");
const pegasusRoot = parseOption(process.argv.slice(2), "--pegasus-root")
  ?? (process.env.RETROM_PEGASUS_ROOT ? path.resolve(process.env.RETROM_PEGASUS_ROOT) : null);

if (!pegasusRoot) {
  throw new Error("Set RETROM_PEGASUS_ROOT or pass --pegasus-root");
}

const manifest = await readManifest();
for (const asset of manifest.assets) {
  const sourceRoot = asset.kind === "runtime" ? runtimeRoot : pegasusRoot;
  const source = path.join(sourceRoot, asset.source);
  const target = path.join(demoRoot, asset.target);
  await verifyFile(source, asset);
  await mkdir(path.dirname(target), { recursive: true });
  await copyFile(source, target);
  await verifyFile(target, asset);
}

console.log(`Prepared and verified ${manifest.assets.length} local demo assets.`);

