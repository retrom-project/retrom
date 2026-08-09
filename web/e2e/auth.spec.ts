import { mkdirSync } from "node:fs";
import path from "node:path";
import { expect, test, type APIRequestContext, type BrowserContext, type Page, type TestInfo } from "@playwright/test";

const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000";
const userPassword = "a sufficiently long password";

function evidencePath(testInfo: TestInfo, name: string) {
  const caseDirectory = process.env.RETROM_ACCEPTANCE_CASE_DIR;
  if (!caseDirectory) return testInfo.outputPath(name);
  const screenshots = path.join(caseDirectory, "screenshots");
  mkdirSync(screenshots, { recursive: true });
  return path.join(screenshots, `${testInfo.project.name}-${name}`);
}

type AuthContext = { csrfToken: string; user: { userId: string; role: "ADMIN" | "USER" } };
type AdminUser = { userId: string; username: string; version: number };

async function login(request: APIRequestContext, username = "test", password = "test") {
  const response = await request.post("/api/v1/auth/login", { data: { username, password }, headers: { Origin: origin } });
  expect(response.ok()).toBe(true);
  return response.json() as Promise<AuthContext>;
}

function writeHeaders(csrfToken: string) {
  return { Origin: origin, "X-Retrom-Csrf": csrfToken };
}

async function createInvitation(request: APIRequestContext, csrfToken: string) {
  const response = await request.post("/api/v1/admin/invitations", {
    data: { role: "USER", confirmAdminRole: false },
    headers: { ...writeHeaders(csrfToken), "Idempotency-Key": crypto.randomUUID() }
  });
  expect(response.status()).toBe(201);
  return (await response.json() as { url: string }).url;
}

async function acceptInvitation(context: BrowserContext, url: string, username: string) {
  const token = new URL(url).hash.replace("#invite=", "");
  const response = await context.request.post("/api/v1/auth/invitations/accept", {
    data: { token, username, displayName: username, password: userPassword, passwordConfirmation: userPassword },
    headers: { Origin: origin }
  });
  expect(response.status()).toBe(201);
  return response.json() as Promise<AuthContext>;
}

async function findUser(request: APIRequestContext, username: string) {
  const response = await request.get(`/api/v1/admin/users?q=${username}&status=ALL&limit=10`);
  expect(response.ok()).toBe(true);
  const users = (await response.json() as { items: AdminUser[] }).items;
  const user = users.find((candidate) => candidate.username === username);
  expect(user).toBeTruthy();
  return user!;
}

async function getUser(request: APIRequestContext, userId: string) {
  const response = await request.get(`/api/v1/admin/users/${userId}`);
  expect(response.ok()).toBe(true);
  return { user: await response.json() as AdminUser, etag: response.headers().etag };
}

async function patchUser(request: APIRequestContext, csrfToken: string, user: AdminUser, etag: string, status: "ENABLED" | "DISABLED") {
  const response = await request.patch(`/api/v1/admin/users/${user.userId}`, {
    data: { status, confirmAdminRole: false },
    headers: { ...writeHeaders(csrfToken), "If-Match": etag, "Idempotency-Key": crypto.randomUUID() }
  });
  expect(response.ok()).toBe(true);
  return { user: await response.json() as AdminUser, etag: response.headers().etag };
}

async function createReset(request: APIRequestContext, csrfToken: string, user: AdminUser, etag: string) {
  const response = await request.post(`/api/v1/admin/users/${user.userId}/password-reset-links`, {
    data: {},
    headers: { ...writeHeaders(csrfToken), "If-Match": etag, "Idempotency-Key": crypto.randomUUID() }
  });
  expect(response.status()).toBe(201);
  return { ...(await response.json() as { url: string; targetUserVersion: number }), etag: response.headers().etag };
}

async function completeReset(page: Page, url: string, password: string) {
  await page.goto(url);
  await expect(page).not.toHaveURL(/#/);
  await expect(page.getByRole("heading", { name: "设置新密码" })).toBeVisible();
  await page.getByLabel("新密码", { exact: true }).fill(password);
  await page.getByLabel("确认密码", { exact: true }).fill(password);
  await page.getByRole("button", { name: "更新密码" }).click();
}

test("ACC-UI-009 authentication entry routing and user management layout remain safe at every desktop viewport", async ({ page }, testInfo: TestInfo) => {
  await page.goto("/library?q=gba");
  await expect(page).toHaveURL(/\/login\?returnTo=%2Flibrary%3Fq%3Dgba$/);
  await expect(page.getByText("测试模式已启用，默认管理员为 test / test。")).toBeVisible();
  await page.getByLabel("用户名").fill("unknown-user");
  await page.getByLabel("密码").fill("not-the-password");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page.locator(".form-error")).toContainText("用户名或密码不正确");
  await page.getByLabel("用户名").fill("test");
  await page.getByLabel("密码").fill("test");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(/\/library\?q=gba$/);

  await page.goto("/admin/users");
  await expect(page.getByRole("heading", { name: "用户管理" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  const ownRow = page.getByRole("row").filter({ hasText: "@test" });
  await ownRow.getByRole("button", { name: "管理" }).click();
  const drawer = page.getByRole("dialog", { name: "管理用户" });
  await expect(drawer).toContainText("不能修改当前登录账号");
  await expect(drawer).toContainText("服务器必须保留至少一名启用管理员");
  await expect(drawer.getByLabel("角色")).toBeDisabled();
  await expect(drawer.getByLabel("状态")).toBeDisabled();
  await page.screenshot({ path: evidencePath(testInfo, "account-and-user-management.png"), fullPage: true });
});

test("ACC-UI-009 an administrator can invite a user without retaining the capability in the UI", async ({ page, browser }, testInfo) => {
  test.skip(testInfo.project.name !== "chrome-1280", "The stateful invitation flow runs once; the layout is covered in every project.");
  await login(page.request);
  await page.goto("/admin/users");
  await page.getByRole("button", { name: "创建邀请" }).click();
  const drawer = page.getByRole("dialog", { name: "创建邀请" });
  await drawer.getByRole("button", { name: "创建邀请" }).click();
  const result = page.getByRole("alertdialog", { name: "邀请已创建" });
  const invitationURL = await result.getByLabel("一次性链接").inputValue();
  expect(invitationURL).toContain("/register#invite=");
  await result.getByRole("button", { name: "完成" }).click();
  await expect(page.getByLabel("一次性链接")).toHaveCount(0);
  await expect(page.locator("body")).not.toContainText(invitationURL);

  const userContext = await browser.newContext({ baseURL: origin });
  const userPage = await userContext.newPage();
  await userPage.goto(invitationURL);
  await expect(userPage).not.toHaveURL(/#/);
  await userPage.getByLabel("用户名", { exact: true }).fill("invited-user");
  await userPage.getByLabel("显示名称").fill("Invited User");
  await userPage.getByLabel("密码", { exact: true }).fill(userPassword);
  await userPage.getByLabel("确认密码", { exact: true }).fill(userPassword);
  await userPage.getByRole("button", { name: "创建账号并进入 Retrom" }).click();
  await expect(userPage.getByRole("heading", { name: "今天想玩什么？" })).toBeVisible();
  await expect(userPage.getByRole("link", { name: "管理后台" })).toHaveCount(0);
  const denied = await userContext.request.get("/api/v1/admin/users");
  expect(denied.status()).toBe(403);
  await userPage.goto("/admin/users");
  await expect(userPage).toHaveURL(/\/admin\/users$/);
  await expect(userPage.getByRole("heading", { name: "没有管理权限" })).toBeVisible();

  const reusedContext = await browser.newContext({ baseURL: origin });
  const reusedPage = await reusedContext.newPage();
  await reusedPage.goto(invitationURL);
  await expect(reusedPage.getByRole("heading", { name: "邀请链接不可用" })).toBeVisible();
  await reusedContext.close();
  await userContext.close();
});

test("ACC-UI-009 password reset revokes old sessions and does not enable a disabled account", async ({ page, browser }, testInfo) => {
  test.setTimeout(60_000);
  test.skip(testInfo.project.name !== "chrome-1280", "The stateful reset flow runs once.");
  const admin = await login(page.request);
  const invitationURL = await createInvitation(page.request, admin.csrfToken);
  const firstUserContext = await browser.newContext({ baseURL: origin });
  await acceptInvitation(firstUserContext, invitationURL, "reset-user");
  const secondUserContext = await browser.newContext({ baseURL: origin });
  await login(secondUserContext.request, "reset-user", userPassword);

  const target = await findUser(page.request, "reset-user");
  let detail = await getUser(page.request, target.userId);
  const enabledReset = await createReset(page.request, admin.csrfToken, detail.user, detail.etag);
  const resetContext = await browser.newContext({ baseURL: origin });
  const resetPage = await resetContext.newPage();
  const changedPassword = "a different sufficiently long password";
  await completeReset(resetPage, enabledReset.url, changedPassword);
  await expect(resetPage.getByRole("heading", { name: "今天想玩什么？" })).toBeVisible();
  expect((await firstUserContext.request.get("/api/v1/home")).status()).toBe(401);
  expect((await secondUserContext.request.get("/api/v1/home")).status()).toBe(401);

  detail = await getUser(page.request, target.userId);
  const disabled = await patchUser(page.request, admin.csrfToken, detail.user, detail.etag, "DISABLED");
  const disabledReset = await createReset(page.request, admin.csrfToken, disabled.user, disabled.etag);
  const disabledResetContext = await browser.newContext({ baseURL: origin });
  const disabledResetPage = await disabledResetContext.newPage();
  const disabledPassword = "a disabled sufficiently long password";
  await completeReset(disabledResetPage, disabledReset.url, disabledPassword);
  await expect(disabledResetPage.getByRole("heading", { name: "账号仍处于停用状态" })).toBeVisible();
  await expect(disabledResetPage.getByText("密码已更新，但账号仍处于停用状态，请联系管理员")).toBeVisible();
  const disabledLogin = await disabledResetContext.request.post("/api/v1/auth/login", {
    data: { username: "reset-user", password: disabledPassword }, headers: { Origin: origin }
  });
  expect(disabledLogin.status()).toBe(401);
  expect(await disabledLogin.text()).not.toContain("DISABLED");

  await disabledResetContext.close();
  await resetContext.close();
  await secondUserContext.close();
  await firstUserContext.close();
});
