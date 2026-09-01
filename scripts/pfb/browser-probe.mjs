import { chromium } from "../../web/node_modules/@playwright/test/index.mjs";

const [executablePath, port, ...hosts] = process.argv.slice(2);
if (!executablePath || !/^\d+$/u.test(port) || hosts.length !== 2) {
  process.exit(2);
}

const browser = await chromium.launch({ executablePath, headless: true });
try {
  const results = [];
  for (const host of hosts) {
    const page = await browser.newPage();
    await page.goto(`http://${host}:${port}/`, { waitUntil: "domcontentloaded", timeout: 15_000 });
    results.push(await page.evaluate(() => ({
      host: location.hostname,
      secure: isSecureContext,
      isolated: crossOriginIsolated,
      sab: typeof SharedArrayBuffer === "function",
    })));
    await page.close();
  }
  process.stdout.write(JSON.stringify(results));
} finally {
  await browser.close();
}
