import { expect, type Locator } from "@playwright/test";

export function serverSourcePath(directory: string): string {
  const source = process.env.RETROM_E2E_SERVER_SOURCE;
  if (!source?.startsWith("/")) {
    throw new Error("RETROM_E2E_SERVER_SOURCE must identify the temporary fixture directory");
  }
  return `${source.slice(1)}/${directory}`;
}

export async function selectServerSource(
  drawer: Locator, directory: string, activate: (entry: Locator) => Promise<void> = (entry) => entry.click(),
) {
  for (const segment of serverSourcePath(directory).split("/")) {
    const entry = drawer.getByRole("button", { name: segment, exact: true });
    const more = drawer.getByRole("button", { name: "加载更多目录", exact: true });
    await expect(entry.or(more).first()).toBeVisible();
    while (!(await entry.isVisible())) {
      await more.click();
      await expect(entry.or(more).first()).toBeVisible();
    }
    await activate(entry);
  }
}
