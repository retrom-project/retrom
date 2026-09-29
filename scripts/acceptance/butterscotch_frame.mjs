export async function visibleButterscotchFrame(capture, options = {}) {
  const {attempts = 50, pause = () => new Promise(resolve => setTimeout(resolve, 200))} = options;
  for (let attempt = 0; attempt < attempts; attempt++) {
    const frame = await capture();
    if (frame.nonBlackPixels >= frame.width * frame.height / 1000) {return frame;}
    if (attempt + 1 < attempts) {await pause();}
  }
  throw new Error("BUTTERSCOTCH_ACCEPTANCE_FRAME_UNAVAILABLE");
}
