import type {NetplayRuntimePort} from "../runtime/netplay-port-adapter";
import type {NetplayDiagnostics} from "./controller-model";
import {digestNetplayState, equalNetplayState} from "./state-digest";

export async function prepareAuthorityTransfer(
  _profileID: string,
  maxStateBytes: number,
  bridge: NetplayRuntimePort,
  diagnostics: NetplayDiagnostics | undefined,
  context: {epoch: number; nextFrame: number},
) {
  const captured = await bridge.captureState(context.nextFrame);
  await bridge.loadStateAndWait(captured, context.nextFrame);
  let state = await bridge.captureState(context.nextFrame);
  diagnostics?.onAuthorityNormalization?.({
    ...context, attempt: 1, expectedCoreBytes: captured.byteLength, recapturedCoreBytes: state.byteLength,
    firstCoreMismatch: firstMismatch(captured, state), lastCoreMismatch: lastMismatch(captured, state),
    coreMismatchCount: mismatchCount(captured, state), coreMismatchRanges: mismatchRanges(captured, state),
  });
  if (!equalNetplayState(captured, state)) {
    await bridge.loadStateAndWait(state, context.nextFrame);
    const fixed = await bridge.captureState(context.nextFrame);
    diagnostics?.onAuthorityNormalization?.({
      ...context, attempt: 2, expectedCoreBytes: state.byteLength, recapturedCoreBytes: fixed.byteLength,
      firstCoreMismatch: firstMismatch(state, fixed), lastCoreMismatch: lastMismatch(state, fixed),
      coreMismatchCount: mismatchCount(state, fixed), coreMismatchRanges: mismatchRanges(state, fixed),
    });
    if (!equalNetplayState(state, fixed)) {throw new Error("STATE_INVALID");}
    state = fixed;
  }
  if (state.byteLength < 1 || state.byteLength > maxStateBytes) {throw new Error("STATE_RING_CAPACITY_EXCEEDED");}
  const stateSha256 = await digestNetplayState(state);
  diagnostics?.onStateCapture?.({
    ...context, byteLength: state.byteLength, stateDigest: stateSha256, coreDigest: stateSha256,
  });
  const recaptured = await bridge.captureState(context.nextFrame);
  return {state, stateSha256, coreSha256: stateSha256, recaptureMatched: equalNetplayState(state, recaptured)};
}

function firstMismatch(left: Uint8Array, right: Uint8Array) {
  const length = Math.max(left.byteLength, right.byteLength);
  for (let index = 0; index < length; index += 1) {if (left[index] !== right[index]) {return index;}}
  return -1;
}

function lastMismatch(left: Uint8Array, right: Uint8Array) {
  for (let index = Math.max(left.byteLength, right.byteLength) - 1; index >= 0; index -= 1) {
    if (left[index] !== right[index]) {return index;}
  }
  return -1;
}

function mismatchCount(left: Uint8Array, right: Uint8Array) {
  let count = 0;
  for (let index = 0; index < Math.max(left.byteLength, right.byteLength); index += 1) {
    if (left[index] !== right[index]) {count += 1;}
  }
  return count;
}

function mismatchRanges(left: Uint8Array, right: Uint8Array) {
  const ranges: Array<{start: number; end: number}> = [];
  for (let index = 0; index < Math.max(left.byteLength, right.byteLength); index += 1) {
    if (left[index] === right[index]) {continue;}
    const tail = ranges.at(-1);
    if (tail?.end === index) {tail.end += 1;}
    else if (ranges.length < 32) {ranges.push({start: index, end: index + 1});}
  }
  return ranges;
}
