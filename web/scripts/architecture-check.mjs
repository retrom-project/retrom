import path from "node:path";
import { fileURLToPath } from "node:url";
import { inspectWeb } from "./architecture/graph.mjs";
import { inspectWebReport, webReportExitCode } from "./architecture/report.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
try {
  const args = process.argv.slice(2);
  const mode = args.length === 0 ? "inventory" : args.length === 2 && args[0] === "--mode" ? args[1] : "";
  if (!["inventory", "check"].includes(mode)) {
    throw new Error("architecture: expected --mode inventory or --mode check");
  }
  const report = mode === "check" ? inspectWebReport(root) : { schemaVersion: 1, files: inspectWeb(root) };
  process.stdout.write(JSON.stringify(report, null, 2) + "\n");
  process.exitCode = mode === "check" ? webReportExitCode(report) : 0;
} catch (error) {
  console.error(error instanceof Error ? error.message.replaceAll(root, "<repository>") : "architecture: analysis error");
  process.exitCode = 2;
}
