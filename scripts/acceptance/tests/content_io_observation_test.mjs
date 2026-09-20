import {test} from "node:test";
import assert from "node:assert/strict";
import {policyContext} from "./content_policy_fixture.mjs";
import {observeContentIO,contentSourceMatcher,rangeSummary} from "../content_io_observation.mjs";
const source={url:"/runtime/content/project/identity/archive.bin",sizeBytes:524288,sha256:"a".repeat(64)};
function request(url,headers={}){return{url:()=>url,method:()=>"GET",resourceType:()=>"fetch",allHeaders:async()=>headers,failure:()=>({errorText:"net::ERR_ABORTED"}),sizes:async()=>({responseBodySize:262144})};}
test("[HP-01] context observer includes Worker responses and whole GETs without storing credentials",async()=>{
 const context=policyContext(),observe=observeContentIO(context,contentSourceMatcher([source],"http://localhost"));
 const req=request("http://localhost"+source.url,{range:"bytes=0-262143",cookie:"secret"});
 context.emit("request",req);context.emit("response",{request:()=>req,status:()=>206,allHeaders:async()=>({"content-range":"bytes 0-262143/524288","content-length":"262144",etag:`"sha256-${source.sha256}"`}),finished:async()=>null});
 const whole=request("http://localhost"+source.url);context.emit("request",whole);context.emit("response",{request:()=>whole,status:()=>200,allHeaders:async()=>({}),finished:async()=>null});
 await observe.flush();assert.equal(observe.requests.length,2);assert.equal(observe.requests[0].resourceType,"fetch");assert.equal(observe.requests[1].sizeBytes,null);assert.equal(observe.requests[1].consumedBytes,null);assert.equal(observe.requests[1].range,null);
 assert.ok(!JSON.stringify(observe.requests).includes("secret"));assert.throws(()=>rangeSummary(observe.requests,source));observe.close();assert.equal(context.listenerCount("request"),0);
});
test("[HP-05] failed requests with no response remain visible; only registered resources are included",async()=>{
 const context=policyContext(),observe=observeContentIO(context,contentSourceMatcher([source],"http://localhost"));
 const req=request("http://localhost"+source.url,{range:"bytes=0-262143"});context.emit("request",req);context.emit("requestfailed",req);
 context.emit("request",request("http://localhost/api/v1/checkpoint"));await observe.flush();assert.equal(observe.requests.length,1);assert.equal(observe.requests[0].status,null);assert.equal(observe.requests[0].failure,"net::ERR_ABORTED");assert.throws(()=>rangeSummary(observe.requests,source));observe.close();
});

test("[HP-05] unfinished Worker response cannot hang evidence collection", async () => {
 const context = policyContext(), observe = observeContentIO(context, () => true);
 const req = request("http://localhost" + source.url);
 context.emit("response", {request: () => req, status: () => 206,
  allHeaders: async () => ({}), finished: () => new Promise(() => {})});
 await assert.rejects(observe.flush({timeoutMs: 10}), /CONTENT_IO_OBSERVATION_TIMEOUT/u);
 observe.close();
});
