import assert from "node:assert/strict";
import {readAudioObservation} from "./rpgmaker_audio_observation.mjs";
import {revealPreviewToolbar, resumePreview} from "./rpgmaker_preview_actions.mjs";

async function audioWindow(opened) {
  await opened.page.waitForTimeout(250);
  await opened.frame.evaluate(() => {
    const value = window.__RETROM_ACCEPTANCE_AUDIO__;
    value.peakAbsoluteSample = 0;
    value.observedSamples = 0;
  });
  await opened.page.waitForTimeout(750);
  return opened.frame.evaluate(() => ({...window.__RETROM_ACCEPTANCE_AUDIO__}));
}

async function openAudioSettings(opened) {
  await revealPreviewToolbar(opened.page);
  await opened.page.getByRole("button", {name: "更多操作", exact: true}).click();
  await opened.page.getByRole("menuitem", {name: "模拟器设置", exact: true}).click();
}

async function resumeAfterSettings(opened) {
  // Settings own the pause until closed; use the same two actions as a player.
  await opened.page.getByRole("button", {name: "收起", exact: true}).click();
  await resumePreview(opened.page);
  return audioWindow(opened);
}

export async function checkBBKRPGAudio(opened) {
  await resumePreview(opened.page);
  const started = await readAudioObservation(opened.page);
  const slider = opened.page.getByRole("slider", {name: "模拟器音量", exact: true});
  await openAudioSettings(opened);
  const unmute = opened.page.getByRole("button", {name: "取消静音", exact: true});
  if (await unmute.isVisible()) {await unmute.click();}
  await slider.fill("100");
  const loud = await resumeAfterSettings(opened);
  assert.ok(loud.peakAbsoluteSample > 0.001, "BBKRPG_AUDIO_SILENT");
  await openAudioSettings(opened);
  await slider.fill("10");
  const quiet = await resumeAfterSettings(opened);
  assert.ok(quiet.peakAbsoluteSample > 0 && quiet.peakAbsoluteSample < loud.peakAbsoluteSample / 2,
    "BBKRPG_VOLUME_INEFFECTIVE");
  await openAudioSettings(opened);
  await slider.fill("100");
  await opened.page.getByRole("button", {name: "静音", exact: true}).click();
  const muted = await resumeAfterSettings(opened);
  assert.equal(muted.peakAbsoluteSample, 0, "BBKRPG_MUTE_INEFFECTIVE");
  await openAudioSettings(opened);
  await opened.page.getByRole("button", {name: "取消静音", exact: true}).click();
  const unmuted = await resumeAfterSettings(opened);
  assert.ok(unmuted.peakAbsoluteSample > 0.001, "BBKRPG_UNMUTE_FAILED");
  await revealPreviewToolbar(opened.page);
  await opened.page.getByRole("button", {name: "暂停", exact: true}).click();
  const paused = await audioWindow(opened);
  assert.equal(paused.peakAbsoluteSample, 0, "BBKRPG_PAUSE_AUDIO_FAILED");
  await resumePreview(opened.page);
  const resumed = await audioWindow(opened);
  assert.ok(resumed.peakAbsoluteSample > 0.001, "BBKRPG_RESUME_AUDIO_FAILED");
  return {started, loud, quiet, muted, unmuted, paused, resumed};
}

export async function measureBBKRPGFrames(opened) {
  return opened.frame.evaluate(async () => {
    const counter = () => window.EJS_emulator.gameManager.Module._get_current_frame_count();
    const start = performance.now(), first = counter(), intervals = [];
    let previous = start;
    await new Promise(resolve => {
      const tick = now => {
        intervals.push(now - previous); previous = now;
        if (now - start >= 5000) {resolve();} else {requestAnimationFrame(tick);}
      };
      requestAnimationFrame(tick);
    });
    const elapsedMs = performance.now() - start, frames = counter() - first;
    intervals.sort((a, b) => a - b);
    return {elapsedMs, frames, fps: frames * 1000 / elapsedMs,
      p95FrameIntervalMs: intervals[Math.floor(intervals.length * .95)], maxFrameIntervalMs: intervals.at(-1)};
  });
}
