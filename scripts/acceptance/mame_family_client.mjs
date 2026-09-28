import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {existsSync, readFileSync, writeFileSync} from "node:fs";
import {join} from "node:path";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {previewCart, approveCart} from "./fantasy_product_client.mjs";
import {waitForTitle} from "./mame_guntus_observe.mjs";
import {canvasDigest} from "./px68k_product_support.mjs";

export const familyCases = {
  atom: {caseId: "ACC-MAME-002", core: "mame_atom", target: "mame-atom", family: "acorn",
    label: "Acorn Atom (MAME)", filename: "Guntus-atom.atm", firmware: 2, width: 372, height: 243},
  pv1000: {caseId: "ACC-MAME-003", core: "mame_pv1000", target: "mame-pv1000", family: "vintage",
    label: "PV-1000 (MAME)", filename: "Guntus-pv1000.rom", firmware: 0, width: 224, height: 244},
};

export async function prepareFamily(client, context, input, evidence) {
  const {profile, directory, source, platform} = input;
  const catalog = await client.json("GET", `/api/v1/admin/bios?scope=FULL_CATALOG&coreId=${profile.core}&limit=100`);
  const requirements = catalog.items.filter(item => item.coreId === profile.core);
  assert.equal(requirements.length, profile.firmware, "MAME_FIRMWARE_CATALOG_MISMATCH");
  for (const item of requirements) {
    if (item.status === "MATCHED") {continue;}
    assert.equal(item.activeInstallation, null);
    const uploadId = await client.upload(singleFile(join(source, item.logicalName)), "FILES", "GENERAL");
    const upload = await client.json("GET", `/api/v1/admin/uploads/${uploadId}`);
    const result = await client.raw("POST", `/api/v1/admin/bios/${item.id}/installations`, {
      headers: {...client.writeHeaders(), "If-Match": `"v${item.version}"`}, data: {uploadFileId: upload.files[0].fileId},
    });
    assert.equal(result.status(), 201, "MAME_FIRMWARE_INSTALL_FAILED");
  }
  const path = join(source, profile.filename), progressPath = join(directory, "progress.json");
  const digest = createHash("sha256").update(readFileSync(path)).digest("hex");
  const progress = existsSync(progressPath) ? JSON.parse(readFileSync(progressPath, "utf8")) : {};
  if (progress.digest) {assert.equal(progress.digest, digest, "MAME_INPUT_CHANGED");}
  let {itemId, gameId} = progress;
  if (!itemId) {
    await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {headers: client.writeHeaders(), data: {}, expected: 200});
    const instances = await client.json("GET", `/api/v1/admin/platform-instances?platformId=${platform}&limit=100`);
    const instance = instances.items.find(item => item.enabled && item.defaultCoreId === profile.core);
    assert.ok(instance, "MAME_PLATFORM_MISSING");
    const uploadId = await client.upload(singleFile(path), "FILES", "GENERAL");
    const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
      data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
    itemId = (await reviewForImport(client, imported.importJobId)).itemId;
    writeFileSync(progressPath, JSON.stringify({digest, itemId}));
  }
  if (!gameId) {
    const preview = await openFamily(context, await previewCart(client, itemId), input, evidence, "preview");
    await preview.canvas.screenshot({path: join(directory, "preview.png")});
    evidence.preview = await canvasDigest(preview.canvas);
    await preview.page.close();
    gameId = (await approveCart(client, itemId)).gameId;
    writeFileSync(progressPath, JSON.stringify({digest, itemId, gameId}));
    evidence.stages.push("import-review-preview-publish");
  } else {evidence.stages.push("reused-published-game");}
  evidence.gameSha256 = digest; evidence.gameId = gameId;
  return gameId;
}

export async function openFamily(context, launch, input, evidence, stage) {
  const page = await context.newPage();
  const cdp = await context.newCDPSession(page);
  await cdp.send("Network.enable"); await cdp.send("Network.setCacheDisabled", {cacheDisabled: true});
  page.on("console", message => {
    if (["warning", "error"].includes(message.type())) {evidence.warnings.push({stage, type: message.type(), text: message.text().slice(0, 300)});}
  });
  page.on("pageerror", error => evidence.errors.push(error.message.slice(0, 200)));
  await page.goto(input.baseUrl + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
  const deadline = Date.now() + 90000;
  while (Date.now() < deadline) {
    const alerts = await page.locator("[role=alert]").allTextContents();
    const error = alerts.join(" ").match(/\b(?:MAME|PLAYER|PROVIDER|RUNTIME)_[A-Z0-9_]+\b/u)?.[0];
    if (error || await page.getByText("RUNTIME_FAILED", {exact: true}).isVisible()) {
      await page.screenshot({path: join(input.directory, `failed-${stage}.png`)}); throw Error(error ?? "MAME_BOOT_FAILED");
    }
    for (const frame of page.frames()) {
      const canvas = frame.locator(`canvas[aria-label="${input.profile.label}"]`);
      if (!await canvas.isVisible()) {continue;}
      await page.getByRole("status").filter({hasText: "可创建存档"}).waitFor({state: "attached", timeout: 30000});
      const config = await page.evaluate(async id => (await fetch(`/runtime/launches/${id}/config`)).json(), launch.launchId ?? launch.previewId);
      assert.equal(config.runtime.targetId, input.profile.target);
      await canvas.click(); await page.waitForTimeout(3000);
      assert.ok((await canvasDigest(canvas)).colors > 1, "MAME_EMPTY_FRAME");
      if (stage !== "restore") {await waitForTitle(canvas, input.platform);}
      console.log(`${input.platform}: ${stage} ready`);
      return {page, canvas, config};
    }
    await page.waitForTimeout(100);
  }
  await page.screenshot({path: join(input.directory, `failed-${stage}.png`)});
  throw Error("MAME_BOOT_TIMEOUT");
}
