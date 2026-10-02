const openedPanels = new WeakSet();

export async function readRuntimeFrames(page) {
  const developmentCount = await page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getFrameCount() ?? null);
  if (developmentCount !== null) {return developmentCount;}
  // Production intentionally exposes no development runtime handle. Read the
  // same real core counter through the ordinary, read-only diagnostics panel.
  const panel = page.getByRole("complementary", {name: "运行调试信息"});
  if (!await panel.isVisible()) {
    await page.locator(".player-hud-handle").hover();
    await page.locator(".player-game-meta").hover();
    await page.getByRole("button", {name: "调试信息", exact: true}).click();
    openedPanels.add(page);
  }
  const text = await panel.getByText("核心帧计数", {exact: true}).locator("..").locator("dd").innerText();
  return /^(?:\d{1,3}(?:,\d{3})*|\d+)$/u.test(text) ? Number(text.replaceAll(",", "")) : null;
}

export async function waitForRuntimeFrameDelta(page, before, minimum, timeoutMs) {
  const deadline = performance.now() + timeoutMs;
  while (performance.now() < deadline) {
    const after = await readRuntimeFrames(page);
    if (Number.isSafeInteger(after) && after - before >= minimum) {return after;}
    await page.waitForTimeout(250);
  }
  throw new Error("RUNTIME_FRAME_OBSERVATION_STALLED");
}

export async function closeFrameObservation(page) {
  if (openedPanels.delete(page)) {
    await page.locator(".player-hud-handle").hover();
    await page.locator(".player-game-meta").hover();
    await page.getByRole("button", {name: "调试信息", exact: true}).click();
  }
}
