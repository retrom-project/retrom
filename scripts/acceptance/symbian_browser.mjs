import assert from "node:assert/strict";
import {writeFileSync} from "node:fs";
import {join} from "node:path";
import {gamepad} from "./fantasy_product_client.mjs";
import {resumePreview} from "./rpgmaker_preview_actions.mjs";
import {computerResources} from "./computer_product_browser.mjs";
import {observeContentIO,contentSourceMatcher} from "./content_io_observation.mjs";
import {proofDigest} from "./content_io_case_proof.mjs";
import sharp from "../../web/node_modules/sharp/dist/index.cjs";

// Operator instrumentation observes the exact factory; it does not replace native behavior.
export async function observeSymbian(context) {
  await context.addInitScript(() => {
    let factory;
    globalThis.__symbianEvents=[];
    globalThis.__symbianAudio=new Map();
    globalThis.__symbianPreloadError=null;
    globalThis.Worker=new Proxy(globalThis.Worker,{construct(Constructor,args,NewTarget){
      const worker=Reflect.construct(Constructor,args,NewTarget);
      if(args[1]?.name==="retrom-content-preload")worker.addEventListener("message",({data})=>{
        if(data?.type==="ERROR")globalThis.__symbianPreloadError=data.code;
      });
      return worker;
    }});
    Object.defineProperty(window,"createEKA2L1",{configurable:true,get(){return factory;},set(value){
      factory=async options=>{
        const callback=options.onCoreEvent;
        options.onCoreEvent=event=>{globalThis.__symbianEvents.push(event);callback?.(event);};
        const audio=options.onAudio;
        options.onAudio=event=>{
          if(event.op==="open")globalThis.__symbianAudio.set(event.pointer,event);
          if(event.op==="close")globalThis.__symbianAudio.delete(event.pointer);
          audio?.(event);
        };
        const module=await value(options);globalThis.__symbianModule=module;return module;
      };
    }});
    globalThis.__symbianProgress=[];
    new MutationObserver(()=>{
      const bar=document.querySelector('[role="progressbar"][aria-label="游戏内容加载进度"]');
      if(bar){const value=Number(bar.getAttribute("aria-valuenow"));if(__symbianProgress.at(-1)!==value)__symbianProgress.push(value);}
    }).observe(document,{subtree:true,childList:true,attributes:true,attributeFilter:["aria-valuenow"]});
  });
}
export async function openSymbian(context,base,launch,directory) {
  const id=launch.launchId??launch.previewId;
  const response=await context.request.get(`${base}/runtime/launches/${id}/config`);
  assert.equal(response.status(),200,"SYMBIAN_LAUNCH_CONFIG_FAILED");
  const config=await response.json(),resources=computerResources(config);
  const network=observeContentIO(context,contentSourceMatcher(resources,base));await network.ready;
  const page=await context.newPage(),errors=[],warnings=[];
  const assets=[],pending=[];
  const assetListener=response=>{
    const path=new URL(response.url()).pathname;
    if(!path.startsWith("/runtime/providers/retrom-runtime/")||!/(?:client\.mjs|worker\.mjs|eka2l1-runtime\.zip)$/u.test(path))return;
    pending.push(response.body().then(bytes=>assets.push({path,sha256:proofDigest(bytes),sizeBytes:bytes.length}),()=>errors.push("SYMBIAN_PROVIDER_ASSET_UNAVAILABLE")));
  };
  context.on("response",assetListener);
  page.on("pageerror",error=>errors.push(error.message));
  page.on("console",message=>{if(message.type()==="warning"||message.type()==="error")warnings.push(message.text());});
  await page.goto(base+launch.playUrl,{waitUntil:"domcontentloaded",timeout:60000});
  let frame;
  for(const deadline=Date.now()+90000;Date.now()<deadline&&!frame;){
    for(const candidate of page.frames()){
      if(await candidate.evaluate(()=>globalThis.__symbianEvents?.some(event=>event.stage==="launched")).catch(()=>false)){frame=candidate;break;}
    }
    const failures=await page.locator("body").innerText();
    if(/(?:EKA2L1|PROVIDER|CONTENT_IO|PLAYER|RUNTIME)_[A-Z_]+/u.test(failures))throw new Error(JSON.stringify({failures,errors,warnings}));
    if(!frame)await page.waitForTimeout(100);
  }
  if(!frame){await page.screenshot({path:join(directory,"startup-failure.png")});throw new Error("SYMBIAN_STARTUP_TIMEOUT");}
  const canvas=frame.locator('canvas[aria-label="Symbian game"]');
  await resumePreview(page);
  assert.equal(config.runtime.targetId,"symbian-eka2l1");assert.equal(config.runtime.checkpoint.semantics,"GAME_SAVE");
  for(let i=0;i<3;i++)await frame.getByRole("button",{name:"Rotate game display"}).click();
  await canvas.click();await page.waitForTimeout(15000);
  await Promise.all(pending);
  assert.ok(assets.some(asset=>asset.path.endsWith("/client.mjs")&&asset.sha256===config.runtime.moduleSha256),"SYMBIAN_MODULE_IDENTITY_MISMATCH");
  return {page,frame,canvas,config,resources,network,launch:{...launch,launchId:id},errors,warnings,assets,
    async flush(){await network.flush();await Promise.all(pending);assert.deepEqual(errors,[]);
      const sandbox="An iframe which has both allow-scripts and allow-same-origin for its sandbox attribute can escape its sandboxing.";
      assert.deepEqual(warnings.filter(message=>message!==sandbox),[]);},
    dispose(){network.close();context.off("response",assetListener);}};
}
export async function pictureSymbian(opened,directory,name) {
  await opened.canvas.screenshot({path:join(directory,name+".png")});
  const data=await opened.canvas.evaluate(canvas=>canvas.toDataURL("image/png"));
  writeFileSync(join(directory,name+"-bitmap.png"),Buffer.from(data.split(",")[1],"base64"));
  return {path:name+".png",bitmapSha256:proofDigest(Buffer.from(data.split(",")[1],"base64"))};
}

export async function marioPosition(opened){
  const data=await opened.canvas.evaluate(canvas=>canvas.toDataURL("image/png"));
  const {data:bytes,info}=await sharp(Buffer.from(data.split(",")[1],"base64")).ensureAlpha().raw().toBuffer({resolveWithObject:true});
  assert.equal(info.width,320);assert.equal(info.height,240);
  // The actual level has the SCORE header; the slot menu also contains a few red text pixels.
  const scoreOutline=Array.from({length:43},(_,x)=>x*4)
    .filter(offset=>bytes[offset]<10&&bytes[offset+1]<10&&bytes[offset+2]<10).length;
  assert.ok(scoreOutline>=16,"SYMBIAN_PLAYER_PIXELS_MISSING");
  // The game's camera shifts the ground vertically while the player jumps.
  const greenGround=Array.from({length:40},(_,row)=>Array.from({length:20},(_,i)=>((200+row)*320+4+i)*4)
    .filter(offset=>bytes[offset]<bytes[offset+1]&&bytes[offset+1]>130&&bytes[offset+2]<75).length);
  assert.ok(greenGround.some(count=>count>6),"SYMBIAN_PLAYER_PIXELS_MISSING");
  const points=[];
  for(let y=100;y<230;y++)for(let x=0;x<320;x++){
    const offset=(y*320+x)*4;
    if(bytes[offset]>190&&bytes[offset+1]<60&&bytes[offset+2]<80)points.push([x,y]);
  }
  assert.ok(points.length>10&&points.length<160,"SYMBIAN_PLAYER_PIXELS_MISSING");
  return {x:points.reduce((sum,p)=>sum+p[0],0)/points.length,y:points.reduce((sum,p)=>sum+p[1],0)/points.length,pixels:points.length};
}

export async function enterMarioLevel(opened,restored=false){
  let map=false;
  for(let i=0;i<7&&!map;i++){
    await pressSymbian(opened,0);await opened.page.waitForTimeout(8000);
    // A saved slot can enter its current level directly after native confirmation.
    try{return {...await marioPosition(opened),entryMode:"LEVEL"};}
    catch(error){if(error.message!=="SYMBIAN_PLAYER_PIXELS_MISSING")throw error;}
    const data=await opened.canvas.evaluate(canvas=>canvas.toDataURL("image/png"));
    const bytes=await sharp(Buffer.from(data.split(",")[1],"base64")).extract({left:8,top:130,width:1,height:1}).removeAlpha().raw().toBuffer();
    map=Math.abs(bytes[0]-24)<=2&&Math.abs(bytes[1]-69)<=2&&Math.abs(bytes[2]-123)<=2;
  }
  assert.ok(map,"SYMBIAN_WORLD_MAP_NOT_REACHED");
  if(!restored)await pressSymbian(opened,13,500);
  await pressSymbian(opened,0);
  for(const deadline=Date.now()+20000;Date.now()<deadline;){
    try{return {...await marioPosition(opened),entryMode:"WORLD_MAP"};}catch(error){if(error.message!=="SYMBIAN_PLAYER_PIXELS_MISSING")throw error;}
    await opened.page.waitForTimeout(500);
  }
  throw new Error("SYMBIAN_LEVEL_LOAD_TIMEOUT");
}

export async function moveMario(opened){
  const before=await marioPosition(opened);await pressSymbian(opened,15,600);
  const after=await marioPosition(opened);assert.ok(after.x>before.x+12,"SYMBIAN_DIRECTION_NOT_APPLIED");
  await pressSymbian(opened,2,250);const jump=await marioPosition(opened);
  assert.ok(jump.y<after.y-10,"SYMBIAN_JUMP_NOT_APPLIED");
  return {before,after,jump,buttons:{confirm:0,right:15,jump:2,cancel:1}};
}

export async function saveMarioInGame(opened){
  await opened.page.waitForTimeout(800);await pressSymbian(opened,1);
  await pressSymbian(opened,13);await pressSymbian(opened,13);await pressSymbian(opened,0);
  await opened.page.waitForTimeout(500);const text=await nativeSlot(opened);
  assert.match(text??"",/^\(supertux-savegame/u);return text;
}
export async function pressSymbian(opened,button,milliseconds=120) {
  await resumePreview(opened.page);await opened.canvas.click();await gamepad(opened.page,button,milliseconds);
}
export async function nativeSlot(opened) {
  return opened.frame.evaluate(()=>{
    const module=globalThis.__symbianModule,path="/data/drives/rm-409/c/data/supertux/save/slot1.stsg";
    return module.FS.analyzePath(path).exists?module.FS.readFile(path,{encoding:"utf8"}):null;
  });
}
export async function measureSymbian(opened,milliseconds=30000) {
  return opened.frame.evaluate(async duration=>{
    const module=globalThis.__symbianModule;
    const probe=document.createElement("canvas"),gl=probe.getContext("webgl2"),extension=gl?.getExtension("WEBGL_debug_renderer_info");
    const renderer=gl&&extension?gl.getParameter(extension.UNMASKED_RENDERER_WEBGL):null;
    gl?.getExtension("WEBGL_lose_context")?.loseContext();
    const sample=()=>({at:performance.now(),frames:module._eka2l1_frames(),bins:Array.from({length:8},(_,i)=>module._eka2l1_metric(i))});
    const tasks=[];const observer=new PerformanceObserver(list=>tasks.push(...list.getEntries().map(entry=>entry.duration)));observer.observe({type:"longtask"});
    const before=sample();await new Promise(resolve=>setTimeout(resolve,duration));const after=sample();observer.disconnect();
    const audio=[...globalThis.__symbianAudio.values()].map(({pointer,rate,channels})=>({rate,channels,
      underruns:Atomics.load(module.HEAPU32,pointer/4+3),active:Atomics.load(module.HEAPU32,pointer/4+2)}));
    return {milliseconds:after.at-before.at,frames:after.frames-before.frames,fps:1000*(after.frames-before.frames)/(after.at-before.at),
      intervalBins:after.bins.map((count,i)=>count-before.bins[i]),intervalUpperBoundsMs:[17,20,25,34,50,100,250,null],
      wasmHeapBytes:module.HEAPU8.byteLength,canvas:[module.canvas.width,module.canvas.height],longTasks:tasks,audio,renderer};
  },milliseconds);
}
