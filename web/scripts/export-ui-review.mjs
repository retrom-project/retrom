import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const designRoot = path.resolve(webRoot, "..", "docs", "design");
const fragment = await readFile(path.join(designRoot, "retrom-ui-review.fragment.html"), "utf8");

function escapeAttribute(value) {
  return value.replaceAll("&", "&amp;").replaceAll('"', "&quot;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

const frameDocument = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline' https://unpkg.com; style-src 'unsafe-inline'; img-src data:; font-src data:; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'">
<title>Retrom UI 交互评审稿</title>
<style>html,body{margin:0;min-height:100%;background:#ebe9e3}body{padding:0}button:focus-visible,input:focus-visible,select:focus-visible,textarea:focus-visible{outline:3px solid rgb(96 69 217 / .45);outline-offset:2px}.sr-only{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}svg.lucide{display:block;width:16px!important;height:16px!important;flex:none;stroke-width:1.6}</style>
<script src="https://unpkg.com/lucide@0.468.0/dist/umd/lucide.min.js"></script>
</head>
<body>
${fragment}
<script>globalThis.lucide?.createIcons({attrs:{width:16,height:16}});</script>
</body>
</html>`;

const document = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline' https://unpkg.com; style-src 'unsafe-inline'; img-src data:; font-src data:; frame-src 'self'; object-src 'none'; base-uri 'none'">
<title>Retrom UI 交互评审稿</title>
<style>:root{color-scheme:light;background:#fff}html,body{margin:0}body{padding:1rem}iframe{display:block;width:100%;height:calc(100vh - 2rem);margin:0;border:0}</style>
</head>
<body>
<iframe sandbox="allow-scripts" referrerpolicy="no-referrer" title="Retrom UI 交互评审稿" srcdoc="${escapeAttribute(frameDocument)}"></iframe>
</body>
</html>
`;

await writeFile(path.join(designRoot, "retrom-ui-review.html"), document);
