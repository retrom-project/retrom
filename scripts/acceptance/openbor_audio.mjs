export async function observeOpenBORAudio(context) {
  await context.addInitScript(() => {
    globalThis.__retromOpenBORAudio = {buffers: 0, nonzeroBuffers: 0};
    const descriptor = Object.getOwnPropertyDescriptor(ScriptProcessorNode.prototype, "onaudioprocess");
    Object.defineProperty(ScriptProcessorNode.prototype, "onaudioprocess", {...descriptor,
      set(callback) {
        descriptor.set.call(this, typeof callback !== "function" ? callback : function(event) {
          callback.call(this, event);
          const evidence = globalThis.__retromOpenBORAudio;
          evidence.buffers++;
          if (event.outputBuffer.getChannelData(0).some(sample => Math.abs(sample) > 0.0001)) evidence.nonzeroBuffers++;
        });
      }});
  });
}
export async function openborAudio(page) {
  const frames = await Promise.all(page.frames().map(frame => frame.evaluate(() => globalThis.__retromOpenBORAudio)));
  return frames.filter(Boolean).reduce((sum, frame) => ({buffers: sum.buffers + frame.buffers,
    nonzeroBuffers: sum.nonzeroBuffers + frame.nonzeroBuffers}), {buffers: 0, nonzeroBuffers: 0});
}
