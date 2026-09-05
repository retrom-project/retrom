// Injected only by Playwright, never imported into the production web bundle.
// Observe a silent side branch without replacing the game's destination edge.
export function installAudioObservation() {
  const evidence = {contexts: 0, observedSamples: 0, peakAbsoluteSample: 0};
  window.__RETROM_ACCEPTANCE_AUDIO__ = evidence;
  if (!globalThis.AudioNode) {return;}
  const connect = AudioNode.prototype.connect;
  const observed = new WeakSet();
  AudioNode.prototype.connect = function (destination, ...args) {
    const result = connect.call(this, destination, ...args);
    if (destination !== this.context.destination || observed.has(this)) {return result;}
    observed.add(this);
    const analyser = this.context.createAnalyser();
    analyser.fftSize = 2048;
    const silence = this.context.createGain();
    silence.gain.value = 0;
    connect.call(this, analyser, args[0] ?? 0);
    connect.call(analyser, silence);
    connect.call(silence, destination);
    evidence.contexts += 1;
    const samples = new Float32Array(analyser.fftSize);
    const timer = setInterval(() => {
      if (this.context.state === "closed") {clearInterval(timer); return;}
      if (this.context.state !== "running") {return;}
      analyser.getFloatTimeDomainData(samples);
      evidence.observedSamples += samples.length;
      for (const sample of samples) {
        if (Number.isFinite(sample)) {
          evidence.peakAbsoluteSample = Math.max(evidence.peakAbsoluteSample, Math.abs(sample));
        }
      }
    }, 25);
    return result;
  };
}

export async function readAudioObservation(page) {
  const result = {contexts: 0, observedSamples: 0, peakAbsoluteSample: 0};
  for (const frame of page.frames()) {
    const value = await frame.evaluate(() => window.__RETROM_ACCEPTANCE_AUDIO__).catch(() => null);
    if (!value) {continue;}
    result.contexts += value.contexts;
    result.observedSamples += value.observedSamples;
    result.peakAbsoluteSample = Math.max(result.peakAbsoluteSample, value.peakAbsoluteSample);
  }
  if (result.contexts < 1 || result.observedSamples < 2048 || result.peakAbsoluteSample <= 0.0001 ||
      !Number.isFinite(result.peakAbsoluteSample)) {
    throw new Error("RPG_PREVIEW_AUDIO_NOT_OBSERVED:" + JSON.stringify(result));
  }
  return result;
}
