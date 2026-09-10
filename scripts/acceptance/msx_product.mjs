import assert from 'node:assert/strict';
import {mkdirSync, writeFileSync} from 'node:fs';
import {join, resolve} from 'node:path';
import {chromium} from '../../web/node_modules/playwright/index.mjs';
import {localRpgAcceptanceProxy} from './rpgmaker_local_proxy.mjs';
import {installVirtualStandardGamepad} from './standard_gamepad.mjs';
import {singleFile, reviewForImport} from './rpgmaker_security_upload.mjs';
import {fantasyClient, previewCart, approveCart, launchCart, gamepad, saveCart} from './fantasy_product_client.mjs';

const baseUrl = process.env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? '.artifacts/msx/product');
mkdirSync(directory, {recursive: true});
const evidence = {schemaVersion: 1, caseId: 'ACC-MSX-001', status: 'FAIL', stages: [], errors: [], diagnostics: [], published: [], games: []};
let browser, proxy;
const required = ['RETROM_ACCEPTANCE_BASE_URL', 'RETROM_ACCEPTANCE_USERNAME', 'RETROM_ACCEPTANCE_PASSWORD',
  'RETROM_CHROME_EXECUTABLE', 'RETROM_MSX_FIXTURE', 'RETROM_MSX_GAMES'];
const missing = required.filter((name) => !process.env[name]);
if (missing.length) {
  writeFileSync(join(directory, 'msx-product.json'), JSON.stringify({...evidence, status: 'BLOCKED', errorCode: 'MSX_ACCEPTANCE_INPUT_REQUIRED', missing}));
  process.exit(3);
}
try {
  proxy = await localRpgAcceptanceProxy(baseUrl);
  browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  evidence.browser = browser.version();
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.on('console', (message) => {
    if (evidence.diagnostics.length < 100) evidence.diagnostics.push(message.text().slice(0, 1500));
  });
  await installVirtualStandardGamepad(context);
  context.setDefaultTimeout(15000);
  const client = await fantasyClient(context, baseUrl);
  const owned = await publish(context, client, process.env.RETROM_MSX_FIXTURE, 'owned');
  await verifyInstantSave(context, client, owned.gameId);
  for (const [index, file] of JSON.parse(process.env.RETROM_MSX_GAMES).entries()) {
    await runExternal(context, client, file, index);
  }
  assert.deepEqual(evidence.errors, []);
  evidence.status = 'PASS';
} catch (error) {
  evidence.errorCode = error.message;
  process.exitCode = 1;
  for (const [index, page] of (browser?.contexts().flatMap((context) => context.pages()) ?? []).entries()) {
    await page.screenshot({path: join(directory, `failure-${index}.png`), timeout: 5000}).catch(() => undefined);
    evidence.failureText = await page.locator('body').innerText().then((text) => text.slice(-1500)).catch(() => '');
  }
} finally {
  await browser?.close();
  await proxy?.close();
  writeFileSync(join(directory, 'msx-product.json'), JSON.stringify(evidence, null, 2) + '\n');
  console.log(JSON.stringify(evidence));
}

async function runExternal(context, client, file, index) {
  const game = await publish(context, client, file, `external-${index}`);
  const launch = await launchCart(client, game.gameId);
  const opened = await open(context, launch, `external-${index}-product`);
  // Real cartridges have different boot/title delays; repeat normal Space presses after the logo.
  for (let attempt = 0; attempt < 4; attempt++) {
    await opened.page.waitForTimeout(1800);
    await gamepad(opened.page, 0, 250);
  }
  await gamepad(opened.page, 9, 300);
  await gamepad(opened.page, 15, 600);
  await opened.page.waitForTimeout(1500);
  await opened.canvas.screenshot({path: join(directory, `external-${index}-after-input.png`), timeout: 10000});
  const result = {gameId: game.gameId, launchId: launch.launchId};
  if (index === 0) {
    const saved = await saveCart(opened.page, launch.launchId, 'webmsx');
    assert.equal(saved.checkpointFormat, 'webmsx-state-v1-storage-v1');
    await opened.page.close();
    const restored = await launchCart(client, game.gameId, saved.saveStateId);
    assert.notEqual(restored.launchId, launch.launchId);
    const next = await open(context, restored, 'external-0-restored');
    await gamepad(next.page, 15, 300);
    await next.canvas.screenshot({path: join(directory, 'external-0-restored-input.png'), timeout: 10000});
    await next.page.close();
    result.saveStateId = saved.saveStateId; result.restoredLaunchId = restored.launchId;
  } else {await opened.page.close();}
  evidence.games.push(result);
}

async function publish(context, client, filename, label) {
  const existing = JSON.parse(process.env.RETROM_MSX_EXISTING_GAMES ?? '{}')[label];
  if (existing) {
    evidence.stages.push(`${label}:reuse-previously-published-product`);
    evidence.published.push({label, gameId: existing, reused: true});
    return {gameId: existing};
  }
  await client.json('POST', '/api/v1/admin/platform-instances/recommendations/apply', {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const platforms = await client.json('GET', '/api/v1/admin/platform-instances?platformId=msx&limit=100');
  const instance = platforms.items.find((item) => item.enabled && item.defaultCoreId === 'webmsx');
  assert.ok(instance, 'MSX_PLATFORM_MISSING');
  const uploadId = await client.upload(singleFile(filename), 'FILES', 'GENERAL');
  const imported = await client.json('POST', '/api/v1/admin/imports', {headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: 'NONE', contentMode: 'STANDARD', tagIds: []}});
  const review = await reviewForImport(client, imported.importJobId);
  const opened = await open(context, await previewCart(client, review.itemId), `${label}-preview`);
  await gamepad(opened.page, 0);
  await opened.page.close();
  const game = await approveCart(client, review.itemId);
  evidence.published.push({label, gameId: game.gameId, reviewId: review.itemId, reused: false});
  evidence.stages.push(`${label}:upload-review-preview-publish`);
  console.log('published:' + label + ':' + game.gameId);
  return game;
}

async function open(context, launch, label) {
  console.log('open:' + label);
  const page = await context.newPage();
  page.on('pageerror', (error) => evidence.errors.push(error.message.slice(0, 250)));
  await page.goto(`${baseUrl}${launch.playUrl}`, {waitUntil: 'domcontentloaded', timeout: 60000});
  for (const deadline = Date.now() + 45000; Date.now() < deadline;) {
    for (const frame of page.frames()) {
      const canvas = frame.locator('canvas[aria-label="MSX game"]');
      if (await canvas.isVisible().catch(() => false)) {
        await canvas.click();
        await page.waitForTimeout(7000);
        await canvas.screenshot({path: join(directory, `${label}.png`), timeout: 10000});
        console.log('canvas:' + label);
        const bounds = await canvas.boundingBox();
        assert.ok(bounds && Math.abs(bounds.width / bounds.height - 4 / 3) < 0.01, 'MSX_DISPLAY_ASPECT_FAILED');
        return {page, canvas};
      }
    }
    const alerts = await page.getByRole('alert').allTextContents();
    if (alerts.some((text) => /WEBMSX_|RUNTIME_FAILED|PROVIDER_/u.test(text))) {throw Error('MSX_RUNTIME_FAILED:' + alerts.join(' '));}
    await page.waitForTimeout(100);
  }
  throw Error('MSX_CANVAS_TIMEOUT');
}

async function verifyInstantSave(context, client, gameId) {
  const original = await launchCart(client, gameId);
  const first = await open(context, original, 'owned-start');
  const initial = await marker(first.canvas);
  await gamepad(first.page, 15, 100);
  const moved = await marker(first.canvas);
  assert.ok(moved.x > initial.x + 10, 'MSX_DIRECTION_FAILED');
  assert.equal(moved.shape, initial.shape, 'MSX_FIXTURE_TRAILS');
  await gamepad(first.page, 1);
  const cancelled = await marker(first.canvas);
  assert.ok(Math.abs(cancelled.x - initial.x) < 2, 'MSX_CANCEL_FAILED');
  await gamepad(first.page, 15, 100);
  const beforeConfirm = await marker(first.canvas);
  await gamepad(first.page, 0);
  const confirmed = await marker(first.canvas);
  assert.notEqual(beforeConfirm.shape, confirmed.shape, 'MSX_CONFIRM_FAILED');
  const saved = await saveCart(first.page, original.launchId, 'webmsx');
  assert.equal(saved.checkpointFormat, 'webmsx-state-v1-storage-v1');
  await first.page.close();
  const restored = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(restored.launchId, original.launchId);
  const next = await open(context, restored, 'owned-restored');
  const restoredMarker = await marker(next.canvas);
  assert.ok(Math.abs(restoredMarker.x - confirmed.x) < 2, 'MSX_RESTORE_POSITION_FAILED');
  assert.equal(restoredMarker.shape, confirmed.shape, 'MSX_RESTORE_STATE_FAILED');
  await gamepad(next.page, 14, 100);
  assert.ok((await marker(next.canvas)).x < restoredMarker.x - 10, 'MSX_RESTORED_INPUT_FAILED');
  await next.page.close();
  const clean = await open(context, await launchCart(client, gameId), 'owned-fresh');
  assert.ok(Math.abs((await marker(clean.canvas)).x - initial.x) < 2, 'MSX_UNREQUESTED_RESTORE');
  await clean.canvas.press('ArrowRight', {delay: 100});
  assert.ok((await marker(clean.canvas)).x > initial.x + 10, 'MSX_KEYBOARD_INPUT_FAILED');
  await verifyLayout(clean);
  await clean.page.close();
  evidence.save = {gameId, originalLaunchId: original.launchId, restoredLaunchId: restored.launchId,
    saveStateId: saved.saveStateId, checkpointFormat: saved.checkpointFormat, initial, moved, confirmed, restoredMarker};
  evidence.stages.push('owned:direction-confirm-cancel-instant-save-fresh-instance-restore-input-isolation');
}

async function verifyLayout(opened) {
  evidence.layout = [];
  for (const viewport of [{width: 1280, height: 901}, {width: 900, height: 1280}, {width: 1280, height: 900}]) {
    await opened.page.setViewportSize(viewport);
    await opened.page.waitForTimeout(400);
    for (let sample = 0; sample < 12; sample++) {
      const rect = await opened.canvas.evaluate((canvas) => {
        const bounds = canvas.getBoundingClientRect();
        return {x: bounds.x, y: bounds.y, width: bounds.width, height: bounds.height,
          viewportWidth: canvas.ownerDocument.defaultView.innerWidth,
          viewportHeight: canvas.ownerDocument.defaultView.innerHeight};
      });
      assert.ok(rect && Math.abs(rect.width / rect.height - 4 / 3) < 0.01, 'MSX_RESIZE_ASPECT_FAILED');
      assert.ok(Math.abs(rect.x * 2 + rect.width - rect.viewportWidth) < 2, 'MSX_RESIZE_HORIZONTAL_FAILED:' + JSON.stringify({rect, viewport}));
      assert.ok(Math.abs(rect.y * 2 + rect.height - rect.viewportHeight) < 2, 'MSX_RESIZE_VERTICAL_FAILED:' + JSON.stringify({rect, viewport}));
      await opened.page.waitForTimeout(30);
    }
    await opened.page.screenshot({path: join(directory, `layout-${viewport.width}-${viewport.height}.png`), timeout: 10000});
    evidence.layout.push(viewport);
  }
}

async function marker(canvas) {
  return canvas.evaluate((element) => {
    const {width, height} = element;
    const pixels = element.getContext('2d').getImageData(0, 0, width, height).data;
    let count = 0, sum = 0, shape = 0;
    for (let y = 0; y < height; y++) {
      for (let x = 0; x < width; x++) {
        const i = (y * width + x) * 4;
        if (pixels[i] > 220 && pixels[i + 1] > 220 && pixels[i + 2] > 220) {
          count++; sum += x; shape += y;
        }
      }
    }
    if (!count || count > 4000) {throw Error('MSX_FIXTURE_MARKER_MISSING:' + count);}
    return {x: sum / count, shape: `${count}:${shape}`, width, height};
  });
}
