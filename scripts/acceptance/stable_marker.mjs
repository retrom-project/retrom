export async function stableMarker(read, wait, now = Date.now) {
  const deadline = now() + 2000;
  let previous = null;
  while (now() < deadline) {
    let current;
    try {current = await read();}
    catch (error) {
      if (!error.message.includes("MSX_FIXTURE_MARKER_MISSING:0")) throw error;
    }
    if (current && previous && current.x === previous.x && current.shape === previous.shape) return current;
    previous = current ?? null;
    await wait(50);
  }
  throw new Error("MSX_FIXTURE_MARKER_UNSTABLE");
}
