import {readFileSync} from "node:fs";
import {expect, test} from "@playwright/test";

const editorStyles = readFileSync(new URL("../features/player/game-editor.css", import.meta.url), "utf8");
const categories = ["金币", "道具", "武器", "护甲", "变量", "开关", "角色", "技能", "状态", "职业", "队伍成员"];

test("expanded game editor categories scroll horizontally without wrapping", async ({page}) => {
  await page.setViewportSize({width: 844, height: 800});
  await page.setContent(`<style>*{box-sizing:border-box}body{margin:0}.game-editor-categories button{flex-shrink:0;min-height:44px;padding:0 18px;white-space:nowrap}</style><style>${editorStyles}</style>
    <div class="game-editor-overlay"><section class="game-editor-panel">
      <header class="game-editor-head"><h1>游戏修改</h1><button>返回游戏</button></header>
      <p class="game-editor-help">修改立即生效。离开前请创建存档。</p>
      <div class="game-editor-category-scroll"><nav class="game-editor-categories" aria-label="修改类别">${categories.map((label) => `<button>${label}</button>`).join("")}</nav><div class="game-editor-category-scrollbar"><span style="width: 75%"></span></div></div>
      <div class="game-editor-list-head"><h2>职业</h2><div><button>查找</button><button>刷新</button></div></div>
      <div class="game-editor-list">战士</div>
    </section></div>`);
  for (const [width, height] of [[844, 800], [844, 390], [390, 844]]) {
    await page.setViewportSize({width, height});
    const geometry = await page.evaluate(() => {
      const overlay = document.querySelector(".game-editor-overlay")!;
      const panel = document.querySelector(".game-editor-panel")!;
      const nav = document.querySelector(".game-editor-categories")!;
      const rail = document.querySelector(".game-editor-category-scrollbar")!;
      const buttons = nav.querySelectorAll("button");
      return {overlayWidth: overlay.clientWidth, overlayScrollWidth: overlay.scrollWidth,
        panelRight: panel.getBoundingClientRect().right, navWidth: nav.clientWidth,
        navScrollWidth: nav.scrollWidth, overflowX: getComputedStyle(nav).overflowX,
        railWidth: rail.getBoundingClientRect().width, railHeight: rail.getBoundingClientRect().height,
        firstButtonTop: buttons[0]!.getBoundingClientRect().top,
        lastButtonTop: buttons[buttons.length - 1]!.getBoundingClientRect().top};
    });
    expect(geometry.overlayScrollWidth, `${width}×${height}`).toBe(geometry.overlayWidth);
    expect(geometry.panelRight, `${width}×${height}`).toBeLessThanOrEqual(width);
    expect(geometry.navScrollWidth, `${width}×${height}`).toBeGreaterThan(geometry.navWidth);
    expect(geometry.overflowX, `${width}×${height}`).toBe("auto");
    expect(geometry.railWidth, `${width}×${height}`).toBe(geometry.navWidth);
    expect(geometry.railHeight, `${width}×${height}`).toBe(8);
    expect(geometry.lastButtonTop, `${width}×${height}`).toBe(geometry.firstButtonTop);
    const nav = page.locator(".game-editor-categories");
    await nav.hover();
    await page.mouse.wheel(300, 0);
    await expect.poll(() => nav.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0);
    await nav.evaluate((element) => {element.scrollLeft = element.scrollWidth;});
    const lastVisible = await nav.evaluate((element) =>
      element.lastElementChild!.getBoundingClientRect().right <= element.getBoundingClientRect().right + 1);
    expect(lastVisible, `${width}×${height}`).toBe(true);
    await nav.evaluate((element) => {element.scrollLeft = 0;});
  }
});
