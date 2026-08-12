import path from "node:path";
import { demoRoot, readManifest, verifyFile } from "./asset-lib.mjs";

const manifest = await readManifest();
for (const asset of manifest.assets) {
  await verifyFile(path.join(demoRoot, asset.target), asset);
}

console.log(`Verified ${manifest.assets.length} local demo assets.`);

