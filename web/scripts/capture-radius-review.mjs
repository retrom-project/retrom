import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium, expect } from "@playwright/test";
import { installGamepads, pressGamepad, standardButton } from "../e2e/immersive-gamepad.ts";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const spec = JSON.parse(await readFile(path.join(root, ".pfb/spec.json"), "utf8"));
const origin = `http://${spec.id}.localhost:3000`;
const output = path.join(root, "web/public/radius-review");
const evidence = path.join(root, ".pfb/evidence/radius-review");
const browser = await chromium.launch({
  executablePath: path.join(root, ".cache/tools/retrom-chrome-for-testing"), headless: true,
});
const sizes = [
  { id: "desktop", title: "1280 × 800", width: 1280, height: 800, dpr: 1 },
  { id: "4k", title: "4K · 150%", width: 2560, height: 1440, dpr: 1.5 },
  { id: "tablet", title: "1024 × 768", width: 1024, height: 768, dpr: 1 },
  { id: "phone", title: "390 × 844", width: 390, height: 844, dpr: 1 },
];
const pages = [
  ["home", "首页", "/", ".home-featured-cover, .phone-game-poster"],
  ["library", "游戏库", "/library", ".library-game-card"],
  ["saves", "存档", "/saves", ".page-header"],
  ["favorites", "收藏", "/favorites", ".page-header"],
  ["recent", "最近游玩", "/recent", ".page-header"],
  ["account", "账户", "/account", ".account-password-form"],
  ["netplay", "联机大厅", "/netplay", ".page-header"],
  ["imports", "入库总览", "/admin/imports", ".page-header"],
  ["import-new", "新建导入", "/admin/imports/new", ".page-header"],
  ["import-server", "本地扫描", "/admin/imports/server", ".page-header"],
  ["import-tasks", "任务", "/admin/imports/tasks", ".page-header"],
  ["reviews", "待审核", "/admin/reviews", ".review-workflow-row"],
  ["history", "审核历史", "/admin/reviews/history", ".page-header"],
  ["admin-games", "游戏管理", "/admin/games", ".page-header"],
  ["tags", "标签管理", "/admin/tags", ".page-header"],
  ["platforms", "游戏目录", "/admin/platform-instances", ".page-header"],
  ["users", "用户管理", "/admin/users", ".page-header"],
  ["bios", "运行依赖", "/admin/bios", ".page-header"],
  ["storage", "容量分析", "/admin/storage", ".page-header"],
];
const captures = [];
const errors = [];
const navigationRetries = [];
const timeout = setTimeout(() => { void browser.close(); }, 360_000);

async function ready(page, route, selector) {
  const context = page.context();
  await page.close();
  let response;
  for (let attempt = 0; attempt < 3; attempt++) {
    page = await context.newPage();
    let readyDocument = false;
    const documentErrors = [];
    page.on("pageerror", (error) => (readyDocument ? errors : documentErrors).push(error.message));
    if (route.startsWith("/immersive")) {await installGamepads(page);}
    response = await page.goto(`${origin}${route}`, { waitUntil: "load" });
    if (response.status() >= 500) {
      // Close before retrying so delayed errors cannot enter the next document's evidence.
      await page.close();
      navigationRetries.push({ route, status: response.status(), errors: documentErrors });
      continue;
    }
    readyDocument = true;
    errors.push(...documentErrors);
    break;
  }
  assert.ok(response.ok(), `${route}: HTTP ${response.status()}`);
  await page.locator(selector).filter({ visible: true }).first().waitFor();
  await page.evaluate(() => document.fonts.ready);
  await page.waitForFunction(() => [...document.images].every((image) => {
    const rect = image.getBoundingClientRect();
    return !rect.width || !rect.height || rect.top >= innerHeight || rect.bottom <= 0
      || rect.left >= innerWidth || rect.right <= 0 || image.complete;
  }));
  if (route.startsWith("/immersive")) {
    await pressGamepad(page, standardButton.a);
    await expect(page.locator('[data-immersive-shell="true"]')).toHaveAttribute("data-controller-state", "ready");
  }
  if (route === "/library") {
    const search = page.getByRole("searchbox", { name: "搜索游戏" });
    await expect(async () => {
      await page.keyboard.press("/");
      await expect(search).toBeFocused({ timeout: 250 });
    }).toPass({ timeout: 10_000 });
    await search.blur();
  }
  await page.mouse.move(0, 0);
  return page;
}

async function capture(page, screen, size) {
  const [id, title, route] = screen;
  const filename = `${id}-${size.id}-current.png`;
  await page.screenshot({ path: path.join(output, filename), animations: "disabled" });
  const measurement = await page.evaluate(() => {
    const tokens = ["cover", "control", "panel", "dialog"].map((role) =>
      getComputedStyle(document.documentElement).getPropertyValue(`--radius-${role}`).trim());
    const elements = [...document.querySelectorAll("body *")].flatMap((element) => {
      const bounds = element.getBoundingClientRect();
      if (!bounds.width || !bounds.height) {return [];}
      const style = getComputedStyle(element);
      const corners = [style.borderTopLeftRadius, style.borderTopRightRadius, style.borderBottomRightRadius, style.borderBottomLeftRadius];
      if (corners.every((corner) => corner === "0px")) {return [];}
      return [{ element: element.getAttribute("class"), corners, width: bounds.width, height: bounds.height }];
    });
    return { tokens, overflow: document.documentElement.scrollWidth > innerWidth, elements };
  });
  assert.deepEqual(measurement.tokens, ["4px", "6px", "8px", "12px"]);
  assert.equal(measurement.overflow, false, `${id}/${size.id}: page overflow`);
  const oversized = measurement.elements.filter(({ corners }) => corners.some((corner) =>
    !corner.includes("%") && Number.parseFloat(corner) > 12 && Number.parseFloat(corner) < 99));
  assert.deepEqual(oversized, [], `${id}/${size.id}: nonstandard rounded corners`);
  captures.push({ id: `${id}-${size.id}`, title: `${title} · ${size.title}`, route, filename, measurement });
  process.stdout.write(`Captured ${id}/${size.id}\n`);
}

async function signIn(page) {
  page = await ready(page, "/login", ".auth-form");
  await page.getByLabel("用户名", { exact: true }).fill("test");
  await page.getByLabel("密码", { exact: true }).fill("test");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(`${origin}/`);
  return page;
}

function gallery() {
  const options = captures.map(({ filename, title }) => `<option value="${filename}">${title}</option>`).join("");
  return `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><link rel="icon" href="data:,">
<title>Retrom 全站圆角预览</title><style>
body{margin:0;padding:24px;background:#f5f3ef;color:#182033;font:14px/1.5 system-ui,sans-serif}
h1{font-size:24px;margin:0 0 6px}p{color:#5f697b}.controls{display:flex;flex-wrap:wrap;gap:18px;align-items:center;margin:18px 0}
select{font:inherit;padding:8px 12px;border:1px solid #cbc8c1;border-radius:6px;background:white}a{color:#4f3dbb}
.image{overflow:auto;border:1px solid #e4e1da;background:white}img{display:block;width:auto;max-width:100%;height:auto}.zoom img{max-width:none}
</style></head><body><h1>Retrom 全站圆角预览</h1>
<p>封面 4px · 控件 6px · 面板 8px · 弹窗 12px。包含登录、用户页面、管理页面、手机与沉浸模式。</p>
<div class="controls"><select id="scene" aria-label="页面与尺寸">${options}</select>
<label><input id="zoom" type="checkbox">原始像素查看</label><a href="/">打开实际页面</a></div>
<div class="image"><img id="capture" src="${captures[0].filename}" alt="${captures[0].title}"></div>
<script src="gallery.js"></script></body></html>`;
}

try {
  await mkdir(output, { recursive: true });
  await mkdir(evidence, { recursive: true });
  for (const size of sizes) {
    const context = await browser.newContext({ viewport: { width: size.width, height: size.height },
      deviceScaleFactor: size.dpr, colorScheme: "light", reducedMotion: "reduce" });
    let page = await context.newPage();
    for (const [id, title, route] of [["login", "登录", "/login"], ["register", "邀请注册", "/register"], ["reset", "密码重置", "/reset-password"]]) {
      page = await ready(page, route, ".auth-panel");
      await capture(page, [id, title, route], size);
    }
    page = await signIn(page);
    const selected = size.id === "desktop" || size.id === "4k" ? pages : pages.slice(0, 7);
    for (const screen of selected) {
      page = await ready(page, screen[2], screen[3]);
      await capture(page, screen, size);
    }
    if (size.id === "desktop" || size.id === "4k") {
      const gameId = await page.evaluate(async () => (await (await fetch("/api/v1/games?limit=1")).json()).items[0].gameId);
      for (const [id, title, route] of [["game", "游戏详情", `/games/${gameId}`], ["admin-game", "游戏管理详情", `/admin/games/${gameId}`]]) {
        page = await ready(page, route, ".admin-game-hero, .game-detail-hero");
        await capture(page, [id, title, route], size);
      }
      page = await ready(page, "/library", ".library-game-card");
      await page.getByRole("button", { name: /的更多操作/ }).first().click();
      await page.getByRole("menuitem", { name: "管理收藏夹" }).click();
      await page.locator(".favorite-dialog").waitFor();
      await capture(page, ["dialog", "收藏夹弹窗", "/library"], size);
      await page.keyboard.press("Escape");
      await page.locator(".favorite-dialog").waitFor({ state: "hidden" });
      for (const [id, title, route] of [["immersive", "沉浸模式", "/immersive"], ["immersive-saves", "沉浸存档", "/immersive/library/saves"]]) {
        page = await ready(page, route, "main");
        await capture(page, [id, title, route], size);
      }
    }
    await context.close();
  }
  assert.deepEqual(errors, [], "Unexpected browser errors");
  await writeFile(path.join(output, "index.html"), gallery());
  await writeFile(path.join(output, "gallery.js"), `
const scene=document.getElementById('scene');scene.addEventListener('change',()=>{const image=document.getElementById('capture');image.src=scene.value;image.alt=scene.selectedOptions[0].textContent;});
document.getElementById('zoom').addEventListener('change',event=>document.querySelector('.image').classList.toggle('zoom',event.target.checked));
`);
  await writeFile(path.join(evidence, "measurements.json"), JSON.stringify({ origin, captures, errors, navigationRetries }, null, 2));
  process.stdout.write(`${JSON.stringify({ url: `${origin}/radius-review/index.html`, captures: captures.length, errors, navigationRetries })}\n`);
} finally {
  clearTimeout(timeout);
  await browser.close();
}
