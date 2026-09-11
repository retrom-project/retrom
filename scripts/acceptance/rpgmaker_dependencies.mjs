#!/usr/bin/env node
import assert from "node:assert/strict";
import {mkdtempSync, readFileSync, writeFileSync, rmSync} from "node:fs";
import {join, resolve} from "node:path";
import {tmpdir} from "node:os";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {expect} from "../../web/node_modules/@playwright/test/index.mjs";
import {createProductClient, directoryFiles, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {normalizedBase} from "./rpgmaker_url.mjs";

const baseUrl = normalizedBase(process.env.RETROM_ACCEPTANCE_BASE_URL);
const caseDir = process.env.RETROM_RPG_CASE_DIR;
const scratch = mkdtempSync(join(tmpdir(), "retrom-resource-policy-"));
const proxy = await localRpgAcceptanceProxy(baseUrl);
const browser = await chromium.launch({executablePath:process.env.RETROM_CHROME_EXECUTABLE,headless:process.env.RETROM_ACCEPTANCE_HEADED !== "1"});
try {
  const context = await browser.newContext({viewport:{width:1440,height:1000},...proxy.contextOptions});
  const response = await context.request.post(`${baseUrl}/api/v1/auth/login`, {
    headers:{Origin:baseUrl},data:{username:process.env.RETROM_ACCEPTANCE_USERNAME,password:process.env.RETROM_ACCEPTANCE_PASSWORD},
  });
  assert.equal(response.status(),200);
  const login = await response.json();
  const client = createProductClient(context,baseUrl,login.csrfToken);
  const retired = await verifyRetiredCapability(client);
  const platformId = await rpgPlatform(client);
  const page = await context.newPage();
  await page.goto(`${baseUrl}/admin/bios?tab=rpgmaker`);
  await expect(page.getByRole("heading",{name:"运行依赖",exact:true})).toBeVisible();
  await expect(page.getByRole("button",{name:"安装运行包",exact:true})).toHaveCount(0);
  await expect(page.getByRole("link",{name:"服务器批量导入 BIOS"})).toBeVisible();
  await page.screenshot({path:join(caseDir,"screenshots/rpgmaker-bios-only.png"),fullPage:true});
  const projects = [];
  for (const generation of ["rpg2000","rpg2003","rpgxp","rpgvx","rpgvxace"]) {
    const files = directoryFiles(resolve("testdata/public-roms/rpgmaker-smoke",generation),`${generation}/`);
    const ordinary = await importReview(client,files,platformId);
    assert.equal(ordinary.canApprove,true);
    const ordinaryPublished = await approve(client,ordinary,201);
    const external = await importReview(client,externalDeclaration(files,generation),platformId);
    assert.equal(external.validation.compatibilityCode,"RPG_EXTERNAL_RTP_REQUIRED");
    assert.equal(external.canApprove,false);
    const rejected = await approve(client,external,409);
    const confirmed = generation === "rpg2000"
      ? await confirmInBrowser(page,client,external) : await confirm(client,external,true);
    assert.equal(confirmed.canApprove,true);
    const cleared = await confirm(client,confirmed,false);
    assert.equal(cleared.canApprove,false);
    const ready = await confirm(client,cleared,true);
    const published = await approve(client,ready,201);
    projects.push({generation:ordinary.rpgMaker.generation,selfContainedGameId:ordinaryPublished.gameId,
      externalItemId:external.itemId,rejectedStatus:409,rejectedCode:rejected.error.code,
      confirmed:true,clearedBlocked:true,confirmedGameId:published.gameId});
  }
  writeFileSync(join(caseDir,"rpgmaker-product.json"),JSON.stringify({schemaVersion:1,caseId:"ACC-RPG-009",status:"PASS",
    retired,projects,screenshots:["screenshots/rpgmaker-bios-only.png","screenshots/rpgmaker-self-contained-confirmation.png"]},null,2)+"\n");
} finally {
  await browser.close();
  await proxy.close();
  rmSync(scratch,{recursive:true,force:true});
}

async function verifyRetiredCapability(client) {
  const results=[];
  for (const [method,path] of [["GET","/api/v1/admin/runtime-asset-packs"],["POST","/api/v1/admin/runtime-asset-packs/installations"],["DELETE","/api/v1/admin/runtime-asset-packs/installations/01980000-0000-7000-8000-000000009996"]]) {
    const response=await client.raw(method,path,{headers:client.writeHeaders()});
    assert.equal(response.status(),404);
    results.push({method,status:response.status()});
  }
  const upload=await client.raw("POST","/api/v1/admin/uploads",{headers:client.writeHeaders(),data:{purpose:"RUNTIME_ASSET_PACK",sourceType:"FILES",files:[{clientFileId:"pack",relativePath:"rtp.zip",sizeBytes:1}]}});
  assert.ok([400,422].includes(upload.status()));
  return {routes:results,uploadStatus:upload.status()};
}

async function rpgPlatform(client) {
  let result=await client.json("GET","/api/v1/admin/platform-instances?platformId=rpgmaker&limit=100");
  let found=result.items.find(item=>item.enabled && item.defaultCoreId==="rpgmaker");
  if (!found) {
    await client.json("POST","/api/v1/admin/platform-instances/recommendations/apply",{headers:client.writeHeaders(),data:{}});
    result=await client.json("GET","/api/v1/admin/platform-instances?platformId=rpgmaker&limit=100");
    found=result.items.find(item=>item.enabled && item.defaultCoreId==="rpgmaker");
  }
  assert.ok(found);
  return found.id;
}

async function importReview(client,files,platformId) {
  const imported=await client.importProject(files,"DIRECTORY",platformId);
  assert.equal(imported.status,202);
  return reviewForImport(client,imported.body.importJobId);
}

function externalDeclaration(files,generation) {
  return files.map(file=>{
    if (!/(?:RPG_RT|Game)\.ini$/iu.test(file.relativePath)) {return file;}
    let content=readFileSync(file.path,"utf8");
    if (generation==="rpg2000" || generation==="rpg2003") {
      assert.match(content,/FullPackageFlag=1/u);
      content=content.replace("FullPackageFlag=1","FullPackageFlag=0");
    } else {
      content=content.replace(/^RTP\d?=.*$/gimu,"").replace("[Game]",`[Game]\n${generation==="rpgxp" ? "RTP1" : "RTP"}=Standard`);
    }
    const path=join(scratch,`${generation}.ini`);
    writeFileSync(path,content);
    return {...file,path,sizeBytes:Buffer.byteLength(content)};
  });
}

async function confirm(client,review,value) {
  await client.json("PATCH",`/api/v1/admin/reviews/${review.itemId}`,{headers:{...client.writeHeaders(),"If-Match":`"v${review.version}"`},data:{rpgSelfContainedOverride:value,tagIds:review.tags.map(tag=>tag.tagId)}});
  return client.json("GET",`/api/v1/admin/reviews/${review.itemId}`);
}

async function confirmInBrowser(page,client,review) {
  await page.goto(`${baseUrl}/admin/reviews/${review.itemId}`);
  const checkbox=page.getByRole("checkbox",{name:"确认项目自包含 RTP"});
  await expect(checkbox).toBeVisible();
  await expect(page.getByRole("button",{name:"通过并发布"})).toBeDisabled();
  const saved=page.waitForResponse(response=>response.request().method()==="PATCH" && response.url().endsWith(`/reviews/${review.itemId}`));
  await checkbox.check();
  assert.equal((await saved).status(),200);
  await expect(page.getByRole("button",{name:"通过并发布"})).toBeEnabled();
  await expect(page.locator(".review-validation-guidance")).toHaveCount(0);
  await page.screenshot({path:join(caseDir,"screenshots/rpgmaker-self-contained-confirmation.png"),fullPage:true});
  return client.json("GET",`/api/v1/admin/reviews/${review.itemId}`);
}

async function approve(client,review,status) {
  return client.json("POST",`/api/v1/admin/reviews/${review.itemId}/approve`,{expected:status,headers:{...client.writeHeaders(),"If-Match":`"v${review.version}"`},data:{}});
}
