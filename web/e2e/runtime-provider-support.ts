import {
  expect, type APIResponse, type BrowserContext, type Page,
} from "@playwright/test";
import type {
  LaunchEnvelopeV1, RuntimeResourceV1, RuntimeStateV1,
} from "../features/player/runtime/contract";

export type RuntimeEnvelope = LaunchEnvelopeV1;

export function runtimeResource(envelope: RuntimeEnvelope, role: string) {
  return envelope.resources.find((resource) => resource.role === role) ?? null;
}

export function runtimeResourceURL(resource: RuntimeResourceV1 | null) {
  if (!resource) {return null;}
  if ("url" in resource) {return resource.url;}
  if ("entryUrl" in resource) {return resource.entryUrl;}
  if ("indexUrl" in resource) {return resource.indexUrl;}
  return null;
}

export function runtimeResourceURLs(resource: RuntimeResourceV1 | null): string[] {
  if (!resource) {return [];}
  const single = runtimeResourceURL(resource);
  if (single) {return [single];}
  if ("files" in resource) {return resource.files.map((file) => file.url);}
  if ("entries" in resource) {return resource.entries.map((entry) => entry.url);}
  return [];
}

export function runtimeFrameCount(page: Page) {
  return page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getFrameCount() ?? 0);
}

export function runtimeState(page: Page): Promise<RuntimeStateV1 | null> {
  return page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getState() ?? null);
}

export function runtimeCheckpoint(page: Page) {
  return page.evaluate(async () => {
    const diagnostics = window.__RETROM_E2E_RUNTIME_V1__;
    if (!diagnostics) {throw new Error("RUNTIME_E2E_DIAGNOSTICS_UNAVAILABLE");}
    return diagnostics.checkpoint();
  });
}

export async function assertLaunchConfigAvailable(
  label: string,
  context: BrowserContext,
  response: APIResponse,
  launch: {launchId: string},
  origin: string,
) {
  const configURL = new URL(`/runtime/launches/${launch.launchId}/config`, origin).href;
  const launchCookie = (await context.cookies(configURL))
    .find((cookie) => cookie.name === `retrom_launch_${launch.launchId}`);
  expect(launchCookie, `${label} launch response headers: ${JSON.stringify(response.headersArray())}`)
    .toMatchObject({path: `/runtime/launches/${launch.launchId}/`, httpOnly: true});
  const configProbe = await context.request.get(configURL);
  expect(configProbe.status(), `${label} launch config: ${await configProbe.text()}`).toBe(200);
}

export async function setEmulatorDirectionalInput(page: Page, pressed: boolean) {
  if (pressed) {
    const interactionSurface = page.frameLocator("iframe.player-frame").locator(".ejs_canvas_parent");
    await interactionSurface.click({position: {x: 64, y: 64}});
    await page.keyboard.down("a");
  } else {
    await page.keyboard.up("a");
  }
}

export async function exitRuntimePlayer(page: Page) {
  await page.mouse.move(20, 20);
  await page.getByRole("button", {name: "返回并退出游戏"}).click();
  const dialog = page.getByRole("alertdialog", {name: "退出游戏？"});
  await expect(dialog).toBeVisible();
  const finished = page.waitForResponse((response) =>
    response.request().method() === "POST"
      && /\/runtime\/launches\/[^/]+\/finish$/.test(response.url()));
  await dialog.getByRole("button", {name: "退出游戏", exact: true}).click();
  expect((await finished).ok()).toBe(true);
  await expect(page).not.toHaveURL(/\/play\/[0-9a-f-]+$/);
  await expect(page.locator(".player-shell")).toHaveCount(0);
}
