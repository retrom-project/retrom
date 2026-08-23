import type { EJSNetplayFrameBridge } from "./ejs-netplay-4.2.3-v1";
import {
  coreStateBytes,
  digestHex,
  transferCoreStateBytes,
  transferStateBytes,
} from "./ejs-netplay-4.2.3-v1";
import type { NetplayDiagnostics } from "./controller-model";

export function acceptsAuthorityNormalization(
  profileID: string,
  expected: Uint8Array,
  result: Awaited<ReturnType<EJSNetplayFrameBridge["loadStateForTransfer"]>>,
) {
  if (result.coreExact) {return true;}
  if (profileID !== "nestopia-423-v1") {return false;}
  const expectedCore = transferCoreStateBytes(coreStateBytes(expected), profileID);
  const recapturedCore = transferCoreStateBytes(coreStateBytes(result.recaptured), profileID);
  return expectedCore.byteLength === recapturedCore.byteLength &&
    expectedCore.every((byte, index) => byte === recapturedCore[index]);
}

export async function prepareAuthorityTransfer(
  profileID: string,
  maxStateBytes: number,
  bridge: EJSNetplayFrameBridge,
  diagnostics: NetplayDiagnostics | undefined,
  context: { epoch: number; nextFrame: number },
) {
  const captured = bridge.captureState();
  const normalized = await bridge.loadStateForTransfer(captured);
  diagnostics?.onAuthorityNormalization?.({ ...context, attempt: 1, ...normalizationEvidence(normalized) });
  let state: Uint8Array = normalized.recaptured;
  if (!acceptsAuthorityNormalization(profileID, captured, normalized)) {
    const fixedPoint = await bridge.loadStateForTransfer(state);
    diagnostics?.onAuthorityNormalization?.({ ...context, attempt: 2, ...normalizationEvidence(fixedPoint) });
    if (!fixedPoint.coreExact) {throw new Error("STATE_INVALID");}
    state = fixedPoint.recaptured;
  }
  if (state.byteLength > maxStateBytes) {throw new Error("STATE_RING_CAPACITY_EXCEEDED");}
  state = transferStateBytes(state, profileID);
  const [stateSha256, coreSha256] = await Promise.all([
    digestHex(state), digestHex(coreStateBytes(state)),
  ]);
  diagnostics?.onStateCapture?.({ ...context, byteLength: state.byteLength, stateDigest: stateSha256, coreDigest: coreSha256 });
  const recaptured = transferStateBytes(bridge.captureState(), profileID);
  return { state, stateSha256, coreSha256, recaptureMatched: equalBytes(state, recaptured) };
}

function normalizationEvidence(result: Awaited<ReturnType<EJSNetplayFrameBridge["loadStateForTransfer"]>>) {
  return {
    expectedCoreBytes: result.expectedCoreBytes,
    recapturedCoreBytes: result.recapturedCoreBytes,
    firstCoreMismatch: result.firstCoreMismatch,
    lastCoreMismatch: result.lastCoreMismatch,
    coreMismatchCount: result.coreMismatchCount,
    coreMismatchRanges: result.coreMismatchRanges,
  };
}

function equalBytes(left: Uint8Array, right: Uint8Array) {
  return left.byteLength === right.byteLength && left.every((value, index) => value === right[index]);
}
