import assert from "node:assert/strict";
import {join} from "node:path";
import {fantasyClient,launchCart} from "./fantasy_product_client.mjs";
import {observeSymbian} from "./symbian_browser.mjs";
import {denyContentWorkerStorage} from "./content_io_storage_denial.mjs";
import {revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

async function noCore(page){
  for(const frame of page.frames()){
    assert.equal(await frame.locator("canvas").count(),0,"SYMBIAN_CORE_STARTED_DURING_PRELOAD");
    assert.equal(await frame.evaluate(()=>!!globalThis.__symbianModule),false);
  }
}

export async function symbianPreloadFailures(browser,proxy,base,gameId,directory){
  const report={};
  for(const scenario of ["cancel","denied"]){
    const context=await browser.newContext({...proxy.contextOptions,viewport:{width:1280,height:900}});
    await observeSymbian(context);const client=await fantasyClient(context,base);
    const launch=await launchCart(client,gameId);launch.returnTo=`/games/${gameId}`;
    const page=await context.newPage(),workers=[],closed=new Set();
    page.on("worker",worker=>{workers.push(worker);worker.on("close",()=>closed.add(worker));});
    let release,fault,requests=0;
    if(scenario==="denied")page.on("request",request=>{if(new URL(request.url()).pathname.startsWith("/runtime/content/"))requests++;});
    try{
      if(scenario==="denied")fault=await denyContentWorkerStorage(context,page,{workerName:"retrom-content-preload"});
      else{
        const waiting=new Promise(resolve=>{release=resolve;});
        await context.route("**/runtime/content/**",async route=>{
          if(++requests===4){await waiting;await route.abort().catch(()=>{});}
          else await route.continue();
        });
      }
      await page.goto(base+launch.playUrl,{waitUntil:"domcontentloaded",timeout:60000});
      if(scenario==="denied"){
        await page.getByText("无法完成本地缓存，请检查浏览器存储权限和可用空间。",{exact:true}).waitFor({timeout:30000});
        const errorCode=await page.evaluate(()=>globalThis.__symbianPreloadError);
        assert.equal(errorCode,"CONTENT_IO_CACHE_UNAVAILABLE");assert.equal(requests,0,"SYMBIAN_CACHE_FAILURE_NETWORK_FALLBACK");
        await noCore(page);report.denied={errorCode,injection:await fault.finish(),coreStarted:false,contentRequests:requests};
        await page.screenshot({path:join(directory,"cache-denied.png")});
        await page.close();
      }else{
        await page.waitForFunction(()=>{
          const bar=document.querySelector('[role="progressbar"][aria-label="游戏内容加载进度"]');
          return bar&&Number(bar.getAttribute("aria-valuenow"))>0&&Number(bar.getAttribute("aria-valuenow"))<100;
        },null,{timeout:30000});
        await noCore(page);
        const percent=Number(await page.getByRole("progressbar").getAttribute("aria-valuenow"));
        await page.screenshot({path:join(directory,"preload-progress.png")});
        // Loading has no mounted core or native save, so Player cancels directly.
        await revealPreviewToolbar(page);
        await page.getByRole("button",{name:"返回并退出游戏",exact:true}).click();
        await page.waitForURL(`${base}${launch.returnTo}`);
        assert.equal(page.frames().length,1);assert.equal(page.workers().length,0);release();
        report.cancel={percentage:percent,requests,coreStarted:false,workersCreated:workers.length,workersClosed:closed.size};
        assert.equal(closed.size,workers.length,"SYMBIAN_PRELOAD_WORKER_LEAK");
      }
    }catch(error){await page.screenshot({path:join(directory,scenario+"-failure.png")}).catch(()=>{});throw error;}
    finally{release?.();await context.close();}
  }
  return report;
}
