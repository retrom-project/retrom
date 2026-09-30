import assert from "node:assert/strict";
import {mkdirSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {createProductClient, reviewForImport, singleFile} from "./rpgmaker_security_upload.mjs";

const origin = process.env.RETROM_ACCEPTANCE_ORIGIN;
assert.ok(origin, "RETROM_ACCEPTANCE_ORIGIN must select the isolated acceptance server");
const directory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/review-current-facts");
mkdirSync(directory, {recursive:true});
const proxy = await localRpgAcceptanceProxy(origin);
const browser = await chromium.launch({executablePath:resolve(".cache/tools/retrom-chrome-for-testing"), headless:true});
const context = await browser.newContext({...proxy.contextOptions, viewport:{width:1280,height:900}});
try {
  const login = await context.request.post(`${origin}/api/v1/auth/login`, {
    headers:{Origin:origin}, data:{username:process.env.RETROM_ACCEPTANCE_USERNAME ?? "test", password:process.env.RETROM_ACCEPTANCE_PASSWORD ?? "test"},
  });
  assert.equal(login.status(),200,"acceptance login");
  const client = createProductClient(context,origin,(await login.json()).csrfToken);
  await client.json("POST","/api/v1/admin/platform-instances/recommendations/apply",{headers:client.writeHeaders(),data:{},expected:200});
  const platforms = await client.json("GET","/api/v1/admin/platform-instances?platformId=nes&limit=100");
  const platform = platforms.items.find(value=>value.defaultCoreId==="fceumm");
  assert.ok(platform,"FCEUmm directory");
  const contentPath = join(directory,"retrom-current-facts.fds");
  writeFileSync(contentPath,`Retrom owned review dependency test vector ${Date.now()}\n`);
  const uploadId = await client.upload(singleFile(contentPath),"FILES","GENERAL");
  const created = await client.json("POST","/api/v1/admin/imports",{
    headers:client.writeHeaders(),expected:202,data:{uploadId,targetPlatformInstanceId:platform.id,metadataProvider:"NONE",contentMode:"STANDARD",tagIds:[]},
  });
  const before = await reviewForImport(client,created.importJobId);
  assert.equal(before.readiness.compatibilityCode,"LAUNCH_BIOS_MISSING");
  assert.equal(before.canApprove,false);
  const page = await context.newPage();
  const route = `${origin}/admin/reviews/${before.itemId}`;
  await page.goto(route);
  await page.getByRole("button",{name:"通过并发布",exact:true}).waitFor();
  assert.equal(await page.getByRole("button",{name:"通过并发布",exact:true}).isDisabled(),true);
  await captureStates(browser,context,page,route,directory,"missing");
  const bios = await client.json("GET","/api/v1/admin/bios?coreId=fceumm&scope=FULL_CATALOG&limit=100");
  const required = before.readiness.dependencySnapshot.bios.find(value=>value.requirementMode!=="OPTIONAL"&&value.installationId===null);
  assert.ok(required,"missing applicable BIOS dependency");
  const requiredName = required.logicalName;
  const requirement = bios.items.find(value=>value.logicalName===requiredName);
  assert.ok(requirement,"current FDS BIOS requirement");
  assert.equal(requirement.activeInstallation,null,"fresh acceptance BIOS state");
  const biosPath = join(directory,requiredName);
  writeFileSync(biosPath,"Retrom owned BIOS installation test vector\n");
  const biosUploadId = await client.upload(singleFile(biosPath),"FILES","GENERAL");
  const biosUpload = await client.json("GET",`/api/v1/admin/uploads/${biosUploadId}`);
  await client.json("POST",`/api/v1/admin/bios/${requirement.id}/installations`,{
    headers:{...client.writeHeaders(),"If-Match":`"v${requirement.version}"`},expected:201,data:{uploadFileId:biosUpload.files[0].fileId},
  });
  const after = await client.json("GET",`/api/v1/admin/reviews/${before.itemId}`);
  assert.equal(after.version,before.version,"BIOS install must not mutate the draft");
  assert.equal(after.readiness.status,"READY");
  assert.equal(after.canApprove,true);
  const refreshed = page.waitForResponse(response=>new URL(response.url()).pathname===`/api/v1/admin/reviews/${before.itemId}`);
  await page.evaluate(()=>window.dispatchEvent(new Event("focus")));
  assert.equal((await refreshed).status(),200);
  await page.getByRole("button",{name:"通过并发布",exact:true}).waitFor();
  await page.waitForFunction(()=>Array.from(document.querySelectorAll("button")).some(button=>button.textContent==="通过并发布"&&!button.disabled));
  await captureStates(browser,context,page,route,directory,"installed");
  writeFileSync(join(directory,"result.json"),JSON.stringify({status:"PASS",itemId:before.itemId,version:before.version,before:before.readiness.compatibilityCode,after:after.readiness.status,viewports:["1280","390","4k-150"]},null,2)+"\n");
  console.log("review current BIOS facts: PASS (real install API, unchanged draft version, focus refresh, three viewports)");
} finally {
  await context.close();
  await browser.close();
  await proxy.close();
}

async function captureStates(browser,context,page,route,directory,state) {
  await page.screenshot({path:join(directory,`1280-${state}.png`),fullPage:true});
  for (const [name,width,height,deviceScaleFactor] of [["390",390,844,1],["4k-150",2560,1440,1.5]]) {
    const view = await browser.newContext({...proxy.contextOptions,storageState:await context.storageState(),viewport:{width,height},deviceScaleFactor});
    try {
      const target = await view.newPage();
      await target.goto(route);
      if (width===390) {
        await target.getByText("请在电脑上管理游戏库",{exact:true}).waitFor();
        assert.equal(await target.getByRole("button",{name:"通过并发布",exact:true}).count(),0,"mobile administration guard");
      } else {
        const approve = target.getByRole("button",{name:"通过并发布",exact:true});
        await approve.waitFor();
        assert.equal(await approve.isDisabled(),state==="missing",`${name} current approval state`);
      }
      assert.equal(await target.evaluate(()=>document.documentElement.scrollWidth<=document.documentElement.clientWidth),true,`${name} overflow`);
      await target.screenshot({path:join(directory,`${name}-${state}.png`),fullPage:true});
    } finally {await view.close();}
  }
}
