import {readFileSync} from "node:fs";
import {expect, test} from "@playwright/test";

const editorStyles = readFileSync(new URL("../features/player/game-editor.css", import.meta.url), "utf8");
const categories = ["金币", "道具", "武器", "护甲", "变量", "开关", "角色", "技能", "状态", "职业", "队伍成员"];

test("expanded game editor categories stay inside a narrow viewport", async ({page}) => {
  await page.setViewportSize({width: 844, height: 390});
  await page.setContent(`<style>*{box-sizing:border-box}body{margin:0}.game-editor-categories button{flex-shrink:0;min-height:44px;padding:0 18px;white-space:nowrap}</style><style>${editorStyles}</style>
    <div class="game-editor-overlay"><section class="game-editor-panel">
      <header class="game-editor-head"><h1>游戏修改</h1><button>返回游戏</button></header>
      <p class="game-editor-help">修改立即生效。离开前请创建存档。</p>
      <nav class="game-editor-categories" aria-label="修改类别">${categories.map((label) => `<button>${label}</button>`).join("")}</nav>
      <div class="game-editor-list-head"><h2>职业</h2><div><button>查找</button><button>刷新</button></div></div>
      <div class="game-editor-list">战士</div>
    </section></div>`);
  const geometry = await page.evaluate(() => {
    const overlay = document.querySelector(".game-editor-overlay")!;
    const panel = document.querySelector(".game-editor-panel")!;
    const nav = document.querySelector(".game-editor-categories")!;
    return {overlayWidth: overlay.clientWidth, overlayScrollWidth: overlay.scrollWidth,
      panelRight: panel.getBoundingClientRect().right, navWidth: nav.clientWidth,
      navScrollWidth: nav.scrollWidth};
  });
  expect(geometry.overlayScrollWidth).toBe(geometry.overlayWidth);
  expect(geometry.panelRight).toBeLessThanOrEqual(844);
  expect(geometry.navScrollWidth).toBeGreaterThan(geometry.navWidth);
});
