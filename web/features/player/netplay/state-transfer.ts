import type {NetplayRuntimePort} from "../runtime/netplay-port-adapter";
import type {NetplayDiagnostics} from "./controller-model";
import {decodeStateFrame, type ServerMessage} from "./protocol";
import {digestNetplayState} from "./state-digest";

export type PendingState = {transferId: string; nextFrame: number; byteLength: number; stateSha256: string; coreSha256: string};

export function pendingStateFromMessage(message: ServerMessage): PendingState {
  if (!message.transferId || !Number.isSafeInteger(message.nextFrame) ||
    !Number.isSafeInteger(message.byteLength) || !message.stateSha256 || !message.coreSha256 ||
    message.byteLength! < 1 || message.byteLength! > 1_048_576) {throw new Error("PROTOCOL_VIOLATION");}
  return {transferId: message.transferId, nextFrame: message.nextFrame!, byteLength: message.byteLength!,
    stateSha256: message.stateSha256, coreSha256: message.coreSha256};
}

export async function applyTransferredState(
  frame: Uint8Array,
  pending: PendingState,
  sessionId: string,
  epoch: number,
  _profileID: string,
  bridge: NetplayRuntimePort,
  diagnostics?: NetplayDiagnostics,
) {
  const decoded = decodeStateFrame(frame);
  const matches = decoded.sessionId === sessionId && decoded.transferId === pending.transferId &&
    decoded.epoch === epoch && decoded.nextFrame === pending.nextFrame &&
    decoded.state.byteLength === pending.byteLength;
  if (!matches) {throw new Error("PROTOCOL_VIOLATION");}
  const stateSha256 = await digestNetplayState(decoded.state);
  if (stateSha256 !== pending.stateSha256 || pending.coreSha256 !== stateSha256) {throw new Error("STATE_INVALID");}
  const beforeDigest = await digestNetplayState(await bridge.captureState(pending.nextFrame));
  await bridge.loadStateAndWait(decoded.state, pending.nextFrame);
  const recaptured = await bridge.captureState(pending.nextFrame);
  const recapturedDigest = await digestNetplayState(recaptured);
  const exact = recapturedDigest === stateSha256;
  diagnostics?.onStateLoad?.({
    epoch, nextFrame: pending.nextFrame, byteLength: decoded.state.byteLength,
    stateDigest: stateSha256, coreDigest: stateSha256, changed: beforeDigest !== recapturedDigest,
    nativeCompletion: true, byteExact: exact, coreExact: exact,
    expectedCoreBytes: decoded.state.byteLength, recapturedCoreBytes: recaptured.byteLength,
    firstCoreMismatch: exact ? -1 : 0,
  });
  if (!exact) {throw new Error("STATE_INVALID");}
  return {stateSha256, coreSha256: stateSha256};
}
