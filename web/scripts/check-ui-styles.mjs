import { readFile, readdir } from "node:fs/promises";
import path from "node:path";
import postcss from "postcss";
import { checkComponent, checkGlobalImports, checkStyles } from "./ui-style-rules.mjs";

const webRoot = path.resolve(import.meta.dirname, "..");
async function filesIn(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const paths = await Promise.all(entries.map((entry) => entry.isDirectory()
    ? filesIn(path.join(directory, entry.name)) : [path.join(directory, entry.name)]));
  return paths.flat();
}

const tokenCSS = await readFile(path.join(webRoot, "styles/tokens.css"), "utf8");
const tokens = new Set();
postcss.parse(tokenCSS).walkDecls((node) => tokens.add(node.prop));
const files = (await Promise.all(["styles", "app", "features", "components", "lib"].map((directory) => filesIn(path.join(webRoot, directory))))).flat();
const errors = [];
for (const file of files) {
  const relative = path.relative(webRoot, file);
  if (file.endsWith(".css")) {
    errors.push(...checkStyles(await readFile(file, "utf8"), relative, tokens));
  } else if (/\.[jt]sx?$/.test(file) && !/\.(test|spec)\./.test(file) && !relative.startsWith("lib/api/generated/")) {
    errors.push(...checkComponent(await readFile(file, "utf8"), relative));
  }
}
errors.push(...checkGlobalImports(await readFile(path.join(webRoot, "app/globals.css"), "utf8")));
if (errors.length) { console.error(errors.join("\n")); process.exitCode = 1; }
else { process.stdout.write("UI style gate: PASS (all product styles and components)\n"); }
