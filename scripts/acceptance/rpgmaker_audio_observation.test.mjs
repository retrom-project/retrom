import assert from "node:assert/strict";
import test from "node:test";
import {readFileSync} from "node:fs";
import {runInNewContext} from "node:vm";
import {installAudioObservation, readAudioObservation} from "./rpgmaker_audio_observation.mjs";

test("audio evidence requires actual non-silent samples, not just an AudioContext", async () => {
  for (const value of [
    null, {contexts: 1, observedSamples: 2048, peakAbsoluteSample: 0},
    {contexts: 1, observedSamples: 0, peakAbsoluteSample: 1},
  ]) {
    await assert.rejects(readAudioObservation({frames: () => [{evaluate: async () => value}]}),
      /RPG_PREVIEW_AUDIO_NOT_OBSERVED/);
  }
});
test("owned isolation fixtures generate a real bounded tone, not an all-zero audio buffer", () => {
  const source = readFileSync(new URL("../../testdata/public-roms/rpgmaker-smoke/malicious-rpgmv/js/main.js", import.meta.url), "utf8");
  const functionSource = source.slice(source.indexOf("function sound()"), source.indexOf('global.addEventListener("keydown"'));
  let samples;
  class Context {
    sampleRate = 48000;
    destination = {};
    createBufferSource() {return {connect() {}, start() {}};}
    createBuffer(_channels, length) {
      samples = new Float32Array(length);
      return {getChannelData: () => samples};
    }
  }
  runInNewContext(functionSource + "\nsound();", {AudioContext: Context});
  assert.ok(samples.length >= 4800 && samples.length <= 48000);
  assert.ok(samples.some((sample) => Math.abs(sample) > 0.01));
});
test("the development tap preserves the original connection and measures a silent side branch", async () => {
  const previous = {window: globalThis.window, AudioNode: globalThis.AudioNode, setInterval: globalThis.setInterval};
  const calls = [];
  let tick;
  class Node {
    constructor(context) {this.context = context;}
    connect(destination, ...args) {calls.push([this, destination, args]); return destination;}
  }
  const context = {
    state: "running", destination: {},
    createAnalyser: () => Object.assign(new Node(context), {
      getFloatTimeDomainData: (array) => {array[0] = -0.25;},
    }),
    createGain: () => Object.assign(new Node(context), {gain: {value: 1}}),
  };
  try {
    globalThis.window = {};
    globalThis.AudioNode = Node;
    globalThis.setInterval = (callback) => {tick = callback; return 1;};
    installAudioObservation();
    const source = new Node(context);
    assert.equal(source.connect(context.destination, 1, 0), context.destination);
    assert.deepEqual(calls[0], [source, context.destination, [1, 0]]);
    assert.equal(calls[3][0].gain.value, 0);
    tick();
    const result = await readAudioObservation({frames: () => [{evaluate: async () => window.__RETROM_ACCEPTANCE_AUDIO__}]});
    assert.deepEqual(result, {contexts: 1, observedSamples: 2048, peakAbsoluteSample: 0.25});
  } finally {
    Object.assign(globalThis, previous);
  }
});
