import assert from "node:assert/strict";

// GUNTUS player pixels in the lower playfield, excluding the HUD and enemies.
// PV-1000's blue cockpit is distinct from white stars, bullets and enemy colors.
export async function fighterPosition(canvas, platform) {
  return canvas.evaluate((element, kind) => {
    const {width, height} = element, pixels = element.getContext("2d").getImageData(0, 0, width, height).data;
    const selected = (x, y) => {
      const i = (y * width + x) * 4;
      return kind === "pv1000" ? pixels[i] < 50 && pixels[i + 1] < 50 && pixels[i + 2] > 200 :
        pixels[i] < 100 && pixels[i + 1] > 100 && pixels[i + 2] < 50;
    };
    if (kind === "pv1000") {
      let count = 0, sumX = 0, sumY = 0;
      for (let y = 180; y < height - 12; y++) {
        for (let x = 5; x < width - 5; x++) {
          if (selected(x, y)) {count++; sumX += x; sumY += y;}
        }
      }
      return count >= 5 && count < 40 ? {x: sumX / count, y: sumY / count, pixels: count} : null;
    }
    // Atom's monochrome playfield ends before the right-hand HUD. Connected
    // components exclude single-pixel stars and the green border.
    const seen = new Set(), components = [];
    for (let y = 185; y < 216; y++) {
      for (let x = 59; x < 266; x++) {
        if (seen.has(y * width + x) || !selected(x, y)) {continue;}
        const queue = [[x, y]], points = [];
        while (queue.length) {
          const [px, py] = queue.pop(), key = py * width + px;
          if (px < 59 || px >= 266 || py < 185 || py >= 216 || seen.has(key) || !selected(px, py)) {continue;}
          seen.add(key); points.push([px, py]);
          for (const dx of [-1, 0, 1]) {for (const dy of [-1, 0, 1]) {if (dx || dy) {queue.push([px + dx, py + dy]);}}}
        }
        if (points.length >= 20 && points.length < 180) {
          components.push({pixels: points.length, x: points.reduce((n, p) => n + p[0], 0) / points.length,
            y: points.reduce((n, p) => n + p[1], 0) / points.length});
        }
      }
    }
    return components.sort((a, b) => b.pixels - a.pixels)[0] ?? null;
  }, platform);
}
export async function waitForFighter(canvas, platform) {
  for (let i = 0; i < 30; i++) {
    const position = await fighterPosition(canvas, platform);
    if (position) {return position;}
    await canvas.page().waitForTimeout(250);
  }
  throw Error("MAME_GUNTUS_FIGHTER_MISSING");
}
export async function familyDisplay(page, canvas, profile) {
  const result = [];
  for (const viewport of [{width: 1280, height: 900}, {width: 800, height: 600}, {width: 1280, height: 900}]) {
    await page.setViewportSize(viewport); await page.waitForTimeout(150);
    const size = await canvas.evaluate(element => {
      const rect = element.getBoundingClientRect();
      return {width: element.width, height: element.height, displayWidth: rect.width, displayHeight: rect.height};
    });
    assert.equal(size.width, profile.width); assert.equal(size.height, profile.height);
    assert.ok(Math.abs(size.displayWidth / size.displayHeight - 4 / 3) < .005, "MAME_DISPLAY_ASPECT_INVALID");
    assert.ok(size.displayWidth <= viewport.width && size.displayHeight <= viewport.height, "MAME_DISPLAY_OVERFLOW");
    result.push({...viewport, canvas: size});
  }
  return result;
}
export async function familyScreenshot(page, width) {
  const captures = (await Promise.all(page.frames().map(frame => frame.evaluate(() => globalThis.__mameScreenshotEvidence ?? [])))).flat();
  assert.ok(captures.some(size => size.width === width && size.height === Math.round(width * 3 / 4)), "MAME_SCREENSHOT_ASPECT_INVALID");
  return captures;
}
export async function shotEvidence(canvas, platform, fighter) {
  return canvas.evaluate((element, {platform, fighter}) => {
    const {width, height} = element, d = element.getContext("2d").getImageData(0, 0, width, height).data;
    let longest = 0;
    for (let x = Math.max(0, Math.floor(fighter.x - 10)); x <= Math.min(width - 1, Math.ceil(fighter.x + 10)); x++) {
      let run = 0;
      for (let y = 55; y < fighter.y - 12; y++) {
        const i = (y * width + x) * 4;
        const bullet = platform === "atom" ? d[i] < 100 && d[i + 1] > 100 && d[i + 2] < 50 :
          d[i] > 220 && d[i + 1] > 220 && d[i + 2] > 220;
        run = bullet ? run + 1 : 0; longest = Math.max(longest, run);
      }
    }
    return {longestVerticalRun: longest};
  }, {platform, fighter});
}

export async function waitForTitle(canvas, platform) {
  for (let attempt = 0; attempt < 40; attempt++) {
    const ready = await canvas.evaluate((element, platform) => {
      const d = element.getContext("2d").getImageData(0, 0, element.width, element.height).data;
      let ink = 0, dark = 0;
      const [left, right] = platform === "atom" ? [59, 265] : [0, 224];
      for (let y = 65; y < 130; y++) {
        for (let x = left; x < right; x++) {
          const i = (y * element.width + x) * 4;
          if (d[i + 1] > 100 && (platform === "atom" ? d[i + 2] < 50 : d[i + 2] > 100)) {ink++;}
          if (d[i] + d[i + 1] + d[i + 2] < 150) {dark++;}
        }
      }
      return ink > 300 && dark > 3000;
    }, platform);
    if (ready) {return;}
    await canvas.page().waitForTimeout(250);
  }
  throw Error("MAME_GUNTUS_TITLE_MISSING");
}
