import { readFileSync } from "node:fs";
import { join } from "node:path";

export const dynamic = "force-static";

export function GET() {
  // Literal paths keep Next's production file tracing limited to these assets.
  const tokens = readFileSync(join(process.cwd(), "styles/tokens.css"), "utf8");
  const offline = readFileSync(join(process.cwd(), "features/pwa/offline.css"), "utf8");
  const controls = readFileSync(join(process.cwd(), "styles/controls.css"), "utf8");
  const icon = readFileSync(join(process.cwd(), "app/icon.svg"), "utf8");
  const styles = `@layer app, controls;${tokens}@layer app{${offline}}@layer controls{${controls}}`;
  const body = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="#101521">
<title>暂时无法连接 · Retrom</title><style>${styles}</style></head><body>
<main><div class="offline-brand" aria-label="Retrom">${icon}<strong>Retrom</strong></div>
<h1>暂时无法连接</h1><p>请检查网络或服务器连接后重试。游玩和同步存档需要连接服务器。</p>
<p>未同步的存档请在恢复连接后，前往“我的存档”重试或导出。</p>
<a class="button" href="/">重新连接</a></main></body></html>`;
  return new Response(body, { headers: {
    "Content-Type": "text/html; charset=utf-8",
    "Cache-Control": "no-cache",
    "Cross-Origin-Opener-Policy": "same-origin",
    "Cross-Origin-Embedder-Policy": "require-corp",
    "Cross-Origin-Resource-Policy": "same-origin",
    "Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'",
    "X-Content-Type-Options": "nosniff",
  } });
}
