import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import type { Schema } from "../lib/api/types";
import { test } from "./clean-refactor-fixtures";

export async function verifyLoginReturn(page: Page, game: Schema<"Game">) {
  const base = new URL(page.url()).origin;
  const destination = `/games/${game.id}?from=shared&filter=%E4%B8%AD%E6%96%87#details`;
  await page.context().clearCookies();
  await page.goto(destination);
  await expect(page).toHaveURL(`${base}/login?returnTo=${encodeURIComponent(destination)}`);
  await page.getByLabel("用户名", { exact: true }).fill(process.env.RETROM_BROWSER_ADMIN_USER ?? "test");
  await page.getByLabel("密码", { exact: true }).fill("wrong-password");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "用户名或密码不正确" })).toHaveText("用户名或密码不正确");
  await expect(page).toHaveURL(`${base}/login?returnTo=${encodeURIComponent(destination)}`);
  await page.reload();
  await page.screenshot({ path: test.info().outputPath("login-return-form.png") });
  await page.getByLabel("用户名", { exact: true }).fill(process.env.RETROM_BROWSER_ADMIN_USER ?? "test");
  await page.getByLabel("密码", { exact: true }).fill(process.env.RETROM_BROWSER_ADMIN_PASSWORD ?? "test");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page).toHaveURL(base + destination);
  await expect(page.getByRole("heading", { level: 1, name: game.title, exact: true })).toBeVisible();
  await page.screenshot({ path: test.info().outputPath("login-return-game.png") });
  await page.reload();
  await expect(page).toHaveURL(base + destination);
  await expect(page.getByRole("heading", { level: 1, name: game.title, exact: true })).toBeVisible();
  await page.goto(`/login?returnTo=${encodeURIComponent(destination)}`);
  await expect(page).toHaveURL(base + destination);
  await page.goto("/login");
  await expect(page).toHaveURL(`${base}/`);
}
