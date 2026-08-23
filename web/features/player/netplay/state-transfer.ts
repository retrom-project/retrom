import type { EJSNetplayFrameBridge } from "./ejs-netplay-4.2.3-v1";
import { coreStateBytes, digestHex, transferCoreStateBytes } from "./ejs-netplay-4.2.3-v1";
import type { NetplayDiagnostics } from "./controller-model";
import { decodeStateFrame, type ServerMessage } from "./protocol";

export type PendingState = { transferId: string; nextFrame: number; byteLength: number; stateSha256: string; coreSha256: string };
type StateTransferBridge = Pick<EJSNetplayFrameBridge, "captureState" | "loadStateForTransfer">;

export function pendingStateFromMessage(message: ServerMessage): PendingState {
  if (!message.transferId || !Number.isSafeInteger(message.nextFrame) ||
    !Number.isSafeInteger(message.byteLength) || !message.stateSha256 || !message.coreSha256 ||
    message.byteLength! < 1 || message.byteLength! > 1_048_576) {throw new Error("PROTOCOL_VIOLATION");}
  return {
    transferId: message.transferId,
    nextFrame: message.nextFrame!,
    byteLength: message.byteLength!,
    stateSha256: message.stateSha256,
    coreSha256: message.coreSha256,
  };
}

export async function applyTransferredState(
  frame: Uint8Array,
  pending: PendingState,
  sessionId: string,
  epoch: number,
  profileID: string,
  bridge: StateTransferBridge,
  diagnostics?: NetplayDiagnostics,
) {
  const decoded = decodeStateFrame(frame);
  const matches = decoded.sessionId === sessionId && decoded.transferId === pending.transferId && decoded.epoch === epoch && decoded.nextFrame === pending.nextFrame && decoded.state.byteLength === pending.byteLength;
  if (!matches) {throw new Error("PROTOCOL_VIOLATION");}
  const expectedCore = transferCoreStateBytes(coreStateBytes(decoded.state), profileID);
  const [stateSha256, coreSha256] = await Promise.all([
    digestHex(decoded.state), digestHex(expectedCore),
  ]);
  if (stateSha256 !== pending.stateSha256 || coreSha256 !== pending.coreSha256) {throw new Error("STATE_INVALID");}
  const beforeCore = transferCoreStateBytes(coreStateBytes(bridge.captureState()), profileID);
  const beforeDigest = await digestHex(beforeCore);
  const loadResult = await bridge.loadStateForTransfer(decoded.state);
  const recapturedCore = transferCoreStateBytes(coreStateBytes(loadResult.recaptured), profileID);
  const recapturedDigest = await digestHex(recapturedCore);
  const coreExact = recapturedDigest === coreSha256;
  diagnostics?.onStateLoad?.({
    epoch, nextFrame: pending.nextFrame,
    byteLength: decoded.state.byteLength, stateDigest: stateSha256, coreDigest: coreSha256,
    changed: beforeDigest !== recapturedDigest, nativeCompletion: true, byteExact: loadResult.byteExact, coreExact,
    expectedCoreBytes: loadResult.expectedCoreBytes, recapturedCoreBytes: loadResult.recapturedCoreBytes,
    firstCoreMismatch: loadResult.firstCoreMismatch,
  });
  if (!coreExact) {throw new Error("STATE_INVALID");}
  return { stateSha256, coreSha256 };
}
