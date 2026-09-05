export async function waitForAvailableCheckpoint(page) {
  // Observing a disabled property must not click the HUD and steal game focus.
  const button = page.getByRole("button", {name: "创建存档", exact: true, includeHidden: true});
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    if (await button.isEnabled().catch(() => false)) {
      return;
    }
    await page.waitForTimeout(100);
  }
  throw new Error("BUTTERSCOTCH_ACCEPTANCE_SAVE_UNAVAILABLE");
}
