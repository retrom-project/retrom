export async function rufflePosition(canvas) {
  const png = await canvas.evaluate(async (element) => {
    const blob = await element.getRootNode().host.ruffle().captureFrame();
    const bytes = new Uint8Array(await blob.arrayBuffer());
    let encoded = "";
    for (let i = 0; i < bytes.length; i += 8192) {encoded += String.fromCharCode(...bytes.subarray(i, i + 8192));}
    return btoa(encoded);
  });
  return canvas.page().evaluate(async (encoded) => {
    const bytes = Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0));
    const image = await createImageBitmap(new Blob([bytes], {type: "image/png"}));
    const surface = document.createElement("canvas"); surface.width = image.width; surface.height = image.height;
    const ctx = surface.getContext("2d"); ctx.drawImage(image, 0, 0); image.close();
    const scale = Math.min(surface.width / 320, surface.height / 240);
    const left = (surface.width - scale * 320) / 2;
    const top = (surface.height - scale * 240) / 2;
    const row = ctx.getImageData(0, Math.floor(top + scale * 105), surface.width, 1).data;
    for (let x = 0; x < surface.width; x++) {
      if (row[x * 4] > 230 && row[x * 4 + 1] > 230 && row[x * 4 + 2] > 230) {return (x - left) / scale;}
    }
    throw Error("RUFFLE_FIXTURE_MARKER_MISSING");
  }, png);
}

export async function initialRuffleSurface(page) {
  const deadline = performance.now() + 60000;
  while (performance.now() < deadline) {
    for (const frame of page.frames()) {
      const canvas = frame.locator("ruffle-player canvas").first();
      if (!await canvas.isVisible()) continue;
      const ready = await frame.evaluate(() => document.querySelector("ruffle-player")?.ruffle().readyState === 2);
      if (!ready) continue;
      const modal = frame.locator("#hardware-acceleration-modal");
      if (await modal.isVisible()) await modal.click({position: {x: 10, y: 200}, timeout: 5000});
      const position = await rufflePosition(canvas);
      if (Math.abs(position - 20) < 2) return {frame, canvas, position};
    }
    const errors = await page.getByRole("alert").allTextContents();
    if (errors.some(value => /RUFFLE_|RUNTIME_FAILED|PROVIDER_/u.test(value))) throw Error("RUFFLE_INITIAL_FRAME_FAILED");
    await page.waitForTimeout(50);
  }
  throw Error("RUFFLE_INITIAL_FRAME_TIMEOUT");
}

export async function focusRuffleSurface(page, opened) {
  // Ruffle may show the software-rendering notice after the first frame.
  // Dismiss the real UI whenever it would intercept the canvas focus click.
  const notice = opened.frame === page.mainFrame() ? page.locator("#hardware-acceleration-modal")
    : page.frameLocator("iframe").locator("#hardware-acceleration-modal");
  let handlerError;
  await page.addLocatorHandler(notice, async modal => {
    try {await modal.click({position: {x: 10, y: 200}, timeout: 5000});}
    catch (error) {handlerError = error;}
  }, {noWaitAfter: true});
  try {
    await opened.canvas.click({timeout: 10000});
    if (handlerError) throw handlerError;
  } catch (error) {throw handlerError ?? error;}
  finally {await page.removeLocatorHandler(notice);}
}
