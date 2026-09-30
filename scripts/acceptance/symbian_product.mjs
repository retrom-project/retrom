import assert from "node:assert/strict";
import {mkdir,writeFile} from "node:fs/promises";
import {join,resolve} from "node:path";
import {randomUUID} from "node:crypto";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient,previewCart,approveCart,launchCart} from "./fantasy_product_client.mjs";
import {computerSource,installComputerBios,importComputer} from "./computer_product_client.mjs";
import {closeComputer,collectComputerExit,saveComputerDisk} from "./computer_product_browser.mjs";
import {performContentIOPlayerExit} from "./content_io_player_exit.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {observeSymbian,openSymbian,pictureSymbian,nativeSlot,measureSymbian,enterMarioLevel,moveMario,saveMarioInGame} from "./symbian_browser.mjs";
import {symbianPreloadFailures} from "./symbian_preload_product.mjs";
import {measureBrowserRSS} from "./content_io_browser_memory.mjs";
import {symbianDebugPanel,symbianColdTransfer,symbianProcessMemory,symbianDraftPresentation} from "./symbian_diagnostics.mjs";

const env=process.env,base=env.RETROM_ACCEPTANCE_BASE_URL;
const directory=resolve(env.RETROM_ACCEPTANCE_CASE_DIR??".artifacts/symbian-product");await mkdir(directory,{recursive:true});
const report={schemaVersion:1,caseId:"ACC-EKA2L1-001",runId:env.RETROM_ACCEPTANCE_RUN_ID??randomUUID(),status:"FAIL",scope:"CANDIDATE_PRODUCT",launches:[]};
let browser,proxy,active;
const stage=async name=>{report.stage=name;console.log(name);await writeFile(join(directory,"symbian-product.json"),JSON.stringify(report,null,2)+"\n");};
const identity=rows=>rows.map(({sha256,sizeBytes})=>`${sha256}:${sizeBytes}`).sort();
try{
  assert.ok(base&&env.RETROM_CHROME_EXECUTABLE&&env.RETROM_SYMBIAN_SIS&&env.RETROM_SYMBIAN_ROM&&env.RETROM_SYMBIAN_RPKG,"SYMBIAN_OPERATOR_INPUT_REQUIRED");
  proxy=await localRpgAcceptanceProxy(base);
  const gpu=env.RETROM_SYMBIAN_GPU??"swiftshader";assert.ok(["swiftshader","vulkan"].includes(gpu));
  const args=["--autoplay-policy=no-user-gesture-required",`--use-angle=${gpu}`,...(gpu==="vulkan"?
    ["--enable-features=Vulkan","--enable-vulkan","--disable-vulkan-surface"]:["--enable-unsafe-swiftshader"])];
  browser=await chromium.launch({executablePath:env.RETROM_CHROME_EXECUTABLE,headless:true,args});
  let context=await browser.newContext({viewport:{width:2560,height:1440},deviceScaleFactor:1.5,...proxy.contextOptions});
  context.setDefaultTimeout(15000);await installVirtualStandardGamepad(context);await observeSymbian(context);
  let collector=await observeContentStoreEvents(context,{retain:true}),client=await fantasyClient(context,base);
  const bios=await installComputerBios(client,"eka2l1",{"Nokia5320.rom":env.RETROM_SYMBIAN_ROM,"Nokia5320.rpkg":env.RETROM_SYMBIAN_RPKG});
  report.sources=[await computerSource(env.RETROM_SYMBIAN_SIS),...bios];
  const total=report.sources.reduce((sum,row)=>sum+row.sizeBytes,0);
  const review=await importComputer(client,"symbian","eka2l1",env.RETROM_SYMBIAN_SIS);
  report.import={itemId:review.itemId,importJobId:review.importJobId};await stage("review-preview");
  const open=async launch=>{
    active=await openSymbian(context,base,launch,directory);
    assert.deepEqual(identity(active.resources),identity(report.sources),"SYMBIAN_CONTENT_IDENTITY_MISMATCH");
    assert.equal(active.config.runtime.checkpoint.writeFormat,"eka2l1-game-save-v1-storage-v1");
    return active;
  };
  const preview=await open(await previewCart(client,review.itemId));
  assert.equal(await nativeSlot(preview),null);
  report.preview={player:await enterMarioLevel(preview),input:await moveMario(preview),screenshot:await pictureSymbian(preview,directory,"preview-input")};
  await preview.network.flush();
  report.previewCache={files:report.sources.length,bytes:total,requests:preview.network.requests,httpCacheDisabled:preview.network.requests.fetchPolicy,
    transfer:symbianColdTransfer(preview,total),startup:preview.startup};
  assert.ok(preview.network.requests.filter(row=>row.method==="GET").every(row=>[200,206].includes(row.status)&&row.failure===null),"SYMBIAN_COLD_CONTENT_FAILED");
  report.launches.push(await closeComputer(preview,base,collector,"GAME_SAVE"));
  report.gameId=(await approveCart(client,review.itemId)).gameId;
  // Product preload gets its own cold profile; Review Preview uses ON_DEMAND by contract.
  await context.close();
  context=await browser.newContext({viewport:{width:2560,height:1440},deviceScaleFactor:1.5,...proxy.contextOptions});
  context.setDefaultTimeout(15000);await installVirtualStandardGamepad(context);await observeSymbian(context);
  collector=await observeContentStoreEvents(context,{retain:true});client=await fantasyClient(context,base);
  await stage("product-launch");
  const launch=await launchCart(client,report.gameId);launch.returnTo=`/games/${report.gameId}`;
  const game=await open(launch);assert.equal(await nativeSlot(game),null,"SYMBIAN_SAVE_RESTORED_WITHOUT_SELECTION");
  report.player=await enterMarioLevel(game);report.before=await pictureSymbian(game,directory,"level-before");
  await stage("product-performance");
  report.performance=await measureSymbian(game);
  report.processMemory=await measureBrowserRSS(browser);
  report.proportionalMemory=await symbianProcessMemory(browser);
  report.debugPanel=await symbianDebugPanel(game);
  report.startup=game.startup;
  assert.ok(report.performance.fps>=30,"SYMBIAN_GAMEPLAY_BELOW_30_FPS");
  assert.equal(report.performance.longTasks.length,0,"SYMBIAN_BROWSER_LONG_TASK");
  assert.ok(report.performance.audio.some(stream=>stream.active===1)&&report.performance.audio.filter(stream=>stream.active===1).every(stream=>stream.underruns===0),"SYMBIAN_AUDIO_UNDERRUN");
  await context.setOffline(true);report.offlineInput=await moveMario(game);
  report.after=await pictureSymbian(game,directory,"level-offline-input");await context.setOffline(false);
  report.nativeSave=await saveMarioInGame(game);report.savedScreenshot=await pictureSymbian(game,directory,"native-save");
  report.draftPresentation=await symbianDraftPresentation(game);
  await game.network.flush();
  assert.equal(game.config.runtime.capabilities.contentLoading,"PRELOAD_ONLY");
  report.fullCache={files:report.sources.length,bytes:total,requests:game.network.requests,httpCacheDisabled:game.network.requests.fetchPolicy,
    transfer:symbianColdTransfer(game,total)};
  await stage("native-save-upload");
  report.save=await performContentIOPlayerExit(game.page,base,launch,()=>saveComputerDisk(game.page,launch.launchId));
  report.launches.push(await collectComputerExit(game,collector));await stage("native-restore");
  const next=await launchCart(client,report.gameId,report.save.saveStateId);next.returnTo=launch.returnTo;
  assert.notEqual(next.launchId,launch.launchId);
  const restored=await open(next);report.nativeRestore=await nativeSlot(restored);
  assert.equal(report.nativeRestore,report.nativeSave,"SYMBIAN_NATIVE_SAVE_RESTORE_MISMATCH");
  report.restoredPlayer=await enterMarioLevel(restored,true);
  await context.setOffline(true);report.restoredInput=await moveMario(restored);await context.setOffline(false);
  report.restoredScreenshot=await pictureSymbian(restored,directory,"native-restored-input");
  report.viewports=[];
  for(const viewport of [{width:1280,height:900},{width:2560,height:1440}]){
    await restored.page.setViewportSize(viewport);await restored.page.waitForTimeout(300);
    const dimensions=await restored.canvas.evaluate(canvas=>({width:canvas.width,height:canvas.height,cssWidth:canvas.getBoundingClientRect().width,cssHeight:canvas.getBoundingClientRect().height}));
    assert.equal(dimensions.width,320);assert.equal(dimensions.height,240);
    assert.ok(Math.abs(dimensions.cssWidth/dimensions.cssHeight-4/3)<0.01);
    report.viewports.push({viewport,dimensions});
  }
  await restored.network.flush();assert.equal(restored.network.requests.length,0,"SYMBIAN_RESTORED_CONTENT_REQUEST");
  report.launches.push(await closeComputer(restored,base,collector,"GAME_SAVE"));
  await stage("preload-failures");
  report.preloadFailures=await symbianPreloadFailures(browser,proxy,base,report.gameId,directory);
  report.browser={version:browser.version(),gpu,args};
  report.consolePolicy={allowedHostMessage:"same-origin blank iframe sandbox warning",runtimeErrors:0,webglWarnings:0};
  report.status="PASS";
}catch(error){report.errorCode=error.message;report.stack=error.stack;process.exitCode=1;
  await active?.page.screenshot({path:join(directory,"failure.png")}).catch(()=>{});
  if(active)await pictureSymbian(active,directory,"failure-native").catch(()=>{});
  if(active){report.errors=active.errors;report.warnings=active.warnings;}
}finally{
  await browser?.close();await proxy?.close();await stage(report.status);
  console.log(JSON.stringify({caseId:report.caseId,status:report.status,errorCode:report.errorCode}));
}
