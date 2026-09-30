import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import {revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

export async function symbianDebugPanel(opened) {
  await revealPreviewToolbar(opened.page);
  const toggle = opened.page.getByRole("button", {name: "调试信息", exact: true});
  await toggle.click();
  const panel = opened.page.getByRole("complementary", {name: "运行调试信息"});
  const fps = panel.getByText("画面呈现率", {exact: true}).locator("..").locator("dd");
  await fps.filter({hasText: /[1-9][0-9]*(?:\.[0-9])? FPS/u}).waitFor({timeout: 5_000});
  const value = parseFloat(await fps.innerText());
  assert.ok(value > 0, "SYMBIAN_DEBUG_PANEL_FPS_ZERO");
  const counter = await opened.page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getFrameCount());
  assert.ok(Number.isSafeInteger(counter) && counter > 0, "SYMBIAN_PUBLIC_FRAME_COUNT_MISSING");
  const before = counter;
  await opened.page.waitForTimeout(1_100);
  const after = await opened.page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getFrameCount());
  assert.ok(after > before, "SYMBIAN_PUBLIC_FRAME_COUNT_STALLED");
  await toggle.click();
  return {fps: value, before, after};
}

export async function symbianProcessMemory(browser) {
  const cdp = await browser.newBrowserCDPSession();
  try {
    const {processInfo} = await cdp.send("SystemInfo.getProcessInfo"), processes = [];
    for (const {id, type} of processInfo) {
      const text = await readFile(`/proc/${id}/smaps_rollup`, "utf8");
      const field = name => {
        const match = new RegExp(`^${name}:\\s+(\\d+) kB$`, "m").exec(text);
        assert.ok(match, "SYMBIAN_PROCESS_MEMORY_UNAVAILABLE");
        return Number(match[1]) * 1024;
      };
      processes.push({type, rssBytes: field("Rss"), pssBytes: field("Pss"), privateBytes: field("Private_Clean") + field("Private_Dirty")});
    }
    return {processes, pssBytes: processes.reduce((total, row) => total + row.pssBytes, 0),
      rssBytes: processes.reduce((total, row) => total + row.rssBytes, 0)};
  } finally {await cdp.detach();}
}

export function symbianColdTransfer(opened, total) {
  const summaries = opened.resources.map(source => {
    const rows = opened.network.requests.filter(row => row.path === new URL(source.url, opened.page.url()).pathname && row.method === "GET");
    assert.equal(rows.length, 1, "SYMBIAN_EAGER_FILE_REDOWNLOADED");
    const [row] = rows;
    assert.equal(row.status, 200); assert.equal(row.failure, null);
    assert.equal(row.range, null); assert.equal(row.contentRange, null);
    assert.equal(row.encoding, "gzip", "SYMBIAN_CONTENT_NOT_COMPRESSED");
    assert.ok(row.etag?.startsWith("W/"), "SYMBIAN_ENCODED_ETAG_INVALID");
    assert.ok(Number.isSafeInteger(row.transferredBytes) && row.transferredBytes > 0, "SYMBIAN_WIRE_BYTES_UNAVAILABLE");
    return {sha256: source.sha256, decodedBytes: source.sizeBytes, wireBytes: row.transferredBytes};
  });
  assert.equal(summaries.reduce((n, row) => n + row.decodedBytes, 0), total);
  const wireBytes = summaries.reduce((n, row) => n + row.wireBytes, 0);
  assert.ok(wireBytes < total * 0.8, "SYMBIAN_TRANSFER_NOT_REDUCED");
  return {resources: summaries, decodedBytes: total, wireBytes};
}

export async function symbianDraftPresentation(opened) {
  await opened.page.waitForFunction(()=>!document.querySelector('.player-sync-status')?.classList.contains('is-busy'),null,{timeout:5_000});
  const states=await opened.page.evaluate(()=>__symbianSaveStates);
  for(let index=0;index<states.length-1;index++){
    if(states[index].busy&&states[index].text?.includes("正在暂存")){
      assert.ok(states[index+1].at-states[index].at<5_000,"SYMBIAN_DRAFT_STAGING_STUCK");
    }
  }
  return states;
}
