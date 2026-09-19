import path from "node:path";
import { fileURLToPath } from "node:url";
import { inspectWeb } from "./architecture/graph.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
try {
  process.stdout.write(`${JSON.stringify({ schemaVersion: 1, files: inspectWeb(root) }, null, 2)}\n`);
} catch (error) {
  console.error(error instanceof Error ? error.message.replaceAll(root, "<repository>") : "architecture: analysis error");
  process.exitCode = 2;
}
