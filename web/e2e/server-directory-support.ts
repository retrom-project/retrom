import { expect, type Locator } from "@playwright/test";

export function serverSourcePath(directory: string): string {
  const source = process.env.RETROM_E2E_SERVER_SOURCE;
  if (!source?.startsWith("/")) {
    throw new Error("RETROM_E2E_SERVER_SOURCE must identify the temporary fixture directory");
  }
  return `${source.slice(1)}/${directory}`;
}

export async function prepareNewSourceScan(drawer: Locator) {
  const format = drawer.getByRole("combobox", { name: "文件组织格式" });
  const edit = drawer.getByRole("button", { name: "返回修改输入" });
  await expect(format.or(edit).first()).toBeVisible({ timeout: 30_000 });
  if (await edit.isVisible()) {
    // The home entry resumes failed diagnostics. Start another scan through
    // the same input editor while retaining the failed plan's history.
    await edit.click();
    await expect(format).toBeVisible();
    await format.selectOption("");
    await drawer.getByRole("button", { name: "根目录", exact: true }).click();
  }
}

export async function selectServerSource(
  drawer: Locator, directory: string, activate: (entry: Locator) => Promise<void> = (entry) => entry.click(),
) {
  for (const segment of serverSourcePath(directory).split("/")) {
    const entry = drawer.getByRole("button", { name: segment, exact: true });
    const more = drawer.getByRole("button", { name: "加载更多目录", exact: true });
    await expect(entry.or(more).first()).toBeVisible({ timeout: 30_000 });
    while (!(await entry.isVisible())) {
      await more.click();
      await expect(entry.or(more).first()).toBeVisible({ timeout: 30_000 });
    }
    await activate(entry);
  }
}
