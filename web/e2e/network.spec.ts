import { expect, test, type Page } from "@playwright/test";

const noncePattern = /'nonce-([^']+)'/;

async function navigationEvidence(page: Page) {
  const consoleErrors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") {consoleErrors.push(message.text());}
  });
  const response = await page.goto("/", { waitUntil: "load" });
  await expect(page.getByRole("main")).toBeVisible();
  expect(response?.ok()).toBe(true);
  expect(response?.headers()["referrer-policy"]).toBe("no-referrer");
  const csp = response?.headers()["content-security-policy"] ?? "";
  const nonce = csp.match(noncePattern)?.[1] ?? "";
  expect(nonce.length).toBeGreaterThanOrEqual(22);
  const scriptNonces = await page.locator("script[nonce]").evaluateAll((scripts) =>
    scripts.map((script) => (script as HTMLScriptElement).nonce),
  );
  expect(scriptNonces.length).toBeGreaterThan(0);
  expect(new Set(scriptNonces)).toEqual(new Set([nonce]));
  expect(await page.evaluate(() => ({
    secureContext: window.isSecureContext,
    isolated: window.crossOriginIsolated,
    sharedArrayBuffer: typeof SharedArrayBuffer,
  }))).toEqual({ secureContext: true, isolated: true, sharedArrayBuffer: "function" });
  expect(consoleErrors).toEqual([]);
  return { csp, nonce };
}

test("same-origin proxy applies fresh nonce and isolation headers", async ({ page, request }) => {
  const first = await navigationEvidence(page);
  const second = await navigationEvidence(page);
  expect(second.nonce).not.toBe(first.nonce);
  expect(first.csp).toContain("'wasm-unsafe-eval'");
  if (process.env.RETROM_E2E_PRODUCTION === "1") {
    expect(first.csp).not.toContain("'unsafe-eval'");
  } else {
    expect(first.csp).toContain("'unsafe-eval'");
  }

  const providerDigest = process.env.RETROM_E2E_EMULATORJS_BUNDLE_SHA256 ?? "";
  expect(providerDigest).toMatch(/^[0-9a-f]{64}$/);
  const providerModule = `/runtime/providers/emulatorjs/${providerDigest}/client.mjs`;
  for (const path of [
    "/api/v1/auth/context",
    providerModule,
  ]) {
    const response = await request.get(path);
    expect(response.ok()).toBe(true);
    expect(response.headers()["cross-origin-opener-policy"]).toBe("same-origin");
    expect(response.headers()["cross-origin-embedder-policy"]).toBe("require-corp");
    expect(response.headers()["x-content-type-options"]).toBe("nosniff");
    expect(response.headers()["referrer-policy"]).toBe("no-referrer");
  }
  const runtime = await request.get(providerModule);
  expect(runtime.headers()["cross-origin-resource-policy"]).toBe("same-origin");

  const internalOrigin = process.env.RETROM_E2E_INTERNAL_ORIGIN;
  if (internalOrigin) {
    const scripts = await page.locator('script[src^="/_next/"]').evaluateAll((elements) =>
      elements.map((element) => (element as HTMLScriptElement).src),
    );
    for (const script of scripts) {
      const response = await request.get(script);
      expect(await response.text()).not.toContain(internalOrigin);
    }
  }
});
