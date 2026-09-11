import { expect, type Page } from "@playwright/test";

export async function verifyBIOSOnlyDependencies(page: Page) {
  await page.goto("/admin/bios?tab=rpgmaker");
  await expect(page.getByRole("heading", {name:"运行依赖", exact:true})).toBeVisible();
  await expect(page.getByRole("button", {name:"安装运行包", exact:true})).toHaveCount(0);
  await expect(page.getByRole("tab", {name:"RPG Maker 运行包"})).toHaveCount(0);
  await expect(page.getByRole("link", {name:"服务器批量导入 BIOS"})).toBeVisible();
  await page.getByText("RPG Maker 核心诊断",{exact:true}).click();
  await expect(page.getByRole("heading", {name:"Runtime Provider / Target"})).toBeVisible();
  await verifyBIOSScopeAlignment(page);
}

export async function verifyBIOSScopeAlignment(page: Page) {
  const viewport = page.viewportSize();
  try {
    for (const width of [1280, 1920, 2560]) {
      await page.setViewportSize({width, height:1440});
      await page.goto("/admin/bios?scope=REQUIRED_BY_LIBRARY");
      const segment = page.getByRole("group", {name:"BIOS 查看范围"});
      await expect(segment).toBeVisible();
      const initial = await segment.boundingBox();
      if (!initial) {throw new Error("BIOS scope tabs are not visible");}
      for (const name of [/完整 BIOS 目录/, /当前游戏库需要/]) {
        const response = page.waitForResponse(value => value.request().method() === "GET" && new URL(value.url()).pathname === "/api/v1/admin/bios");
        await page.getByRole("button", {name}).click();
        expect((await response).ok()).toBe(true);
        await expect(page.getByRole("heading", {name:"正在加载 BIOS 目录"})).toHaveCount(0);
        const changed = await segment.boundingBox();
        if (!changed) {throw new Error("BIOS scope tabs disappeared during switching");}
        expect(Math.abs(changed.x - initial.x)).toBeLessThanOrEqual(1);
        expect(Math.abs(changed.width - initial.width)).toBeLessThanOrEqual(1);
      }
    }
  } finally {
    if (viewport) {await page.setViewportSize(viewport);}
  }
}
